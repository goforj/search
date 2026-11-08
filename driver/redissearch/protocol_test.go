package redissearch

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
	"github.com/goforj/search/searchtest"
)

// writeReply produces native RESP frames, preserving malformed payload tests above the transport parser.
func writeReply(w *bufio.Writer, value any) error {
	switch v := value.(type) {
	case nil:
		_, err := w.WriteString("$-1\r\n")
		return err
	case nativeError:
		_, err := fmt.Fprintf(w, "-%s\r\n", v)
		return err
	case string:
		_, err := fmt.Fprintf(w, "$%d\r\n%s\r\n", len(v), v)
		return err
	case int64:
		_, err := fmt.Fprintf(w, ":%d\r\n", v)
		return err
	case []any:
		if _, err := fmt.Fprintf(w, "*%d\r\n", len(v)); err != nil {
			return err
		}
		for _, child := range v {
			if err := writeReply(w, child); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported fixture reply %T", value)
	}
}

// redisFixture serves multiple independent commands with no Redis process or persistent state.
func redisFixture(t *testing.T, respond func([]string) any) (*Driver, search.Definition) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(2 * time.Second))
				r := reader{input: bufio.NewReader(conn), remaining: maxResponseBytes}
				value, err := r.value(0)
				if err != nil {
					t.Error(err)
					return
				}
				raw, ok := value.([]any)
				if !ok {
					t.Error("invalid command shape")
					return
				}
				args := make([]string, len(raw))
				for n := range raw {
					args[n], ok = raw[n].(string)
					if !ok {
						t.Error("non-string command argument")
						return
					}
				}
				w := bufio.NewWriter(conn)
				if err := writeReply(w, respond(args)); err != nil {
					t.Error(err)
				}
				if err := w.Flush(); err != nil {
					t.Error(err)
				}
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); wg.Wait() })
	d, err := New(Config{Addr: listener.Addr().String()})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := search.NewIndex(d, searchtest.Definition())
	if err != nil {
		t.Fatal(err)
	}
	def := idx.Definition()
	d.name = d.target(def)
	d.fingerprint, err = def.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return d, def
}

// redisHit represents a native response independently of the driver's projection compiler.
func redisHit(name, id, score, body string) []any {
	return []any{name + ":doc:" + driverutil.IDKey(search.DocumentID(id)), score, []any{"_forjoriginal", id, "_forjbody", body}}
}

// TestSearchProtocol checks complete result collection, portable ordering, facets, and corrupt responses.
func TestSearchProtocol(t *testing.T) {
	cases := []struct {
		name   string
		change func([]any) any
		want   error
	}{
		{name: "valid"},
		{name: "non-array", change: func([]any) any { return "wrong" }, want: search.ErrUnavailable},
		{name: "empty", change: func([]any) any { return []any{} }, want: search.ErrUnavailable},
		{name: "noninteger-count", change: func(r []any) any { r[0] = "2"; return r }, want: search.ErrUnavailable},
		{name: "negative-count", change: func(r []any) any { r[0] = int64(-1); return r }, want: search.ErrUnavailable},
		{name: "too-many", change: func(r []any) any { r[0] = int64(MaxMatches + 1); return r }, want: search.ErrUnsupported},
		{name: "missing-hit", change: func(r []any) any { return r[:4] }, want: search.ErrUnavailable},
		{name: "invalid-key", change: func(r []any) any { r[1] = int64(3); return r }, want: search.ErrUnavailable},
		{name: "invalid-score-type", change: func(r []any) any { r[2] = int64(3); return r }, want: search.ErrUnavailable},
		{name: "nan-score", change: func(r []any) any { r[2] = "NaN"; return r }, want: search.ErrUnavailable},
		{name: "infinite-score", change: func(r []any) any { r[2] = "+Inf"; return r }, want: search.ErrUnavailable},
		{name: "invalid-score", change: func(r []any) any { r[2] = "bad"; return r }, want: search.ErrUnavailable},
		{name: "invalid-fields", change: func(r []any) any { r[3] = []any{"odd"}; return r }, want: search.ErrUnavailable},
		{name: "invalid-id", change: func(r []any) any { r[3].([]any)[1] = int64(1); return r }, want: search.ErrUnavailable},
		{name: "invalid-body-type", change: func(r []any) any { r[3].([]any)[3] = int64(1); return r }, want: search.ErrUnavailable},
		{name: "invalid-body", change: func(r []any) any { r[3].([]any)[3] = "[]"; return r }, want: search.ErrUnavailable},
		{name: "foreign-key", change: func(r []any) any { r[1] = "foreign"; return r }, want: search.ErrUnavailable},
		{name: "duplicate-id", change: func(r []any) any { r[4] = r[1]; r[6] = r[3]; return r }, want: search.ErrUnavailable},
		{name: "server-error", change: func([]any) any { return nativeError("private failure") }, want: search.ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var name string
			d, def := redisFixture(t, func(args []string) any {
				if args[0] != "FT.SEARCH" || !strings.Contains(strings.Join(args, " "), "VERBATIM NOSTOPWORDS") {
					t.Error(args)
				}
				result := []any{int64(2)}
				result = append(result, redisHit(name, "one", "2", `{"title":"red","tags":["a","a"],"category":"cup","active":true,"rank":2,"code":"a"}`)...)
				result = append(result, redisHit(name, "two", "1", `{"title":"red","tags":["a","b"],"active":false,"rank":1,"code":"b"}`)...)
				if tc.change != nil {
					return tc.change(result)
				}
				return result
			})
			name = d.name
			q := search.Query{Text: "red", Fields: []search.FieldName{"title"}, Limit: 10, Facets: []search.TermFacet{{Name: "tags", Field: "tags", Limit: 1}, {Name: "categories", Field: "category", Limit: 2}, {Name: "active", Field: "active", Limit: 2}}}
			r, err := d.Search(context.Background(), def, q)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err == nil {
				if len(r.Hits) != 2 || r.Hits[0].Document.ID != "one" || len(r.Facets) != 3 || r.Facets[0].Buckets[0].Count != 2 || !r.Facets[0].Truncated {
					t.Fatal(r)
				}
			}
		})
	}
	for _, text := range []string{"", "red"} {
		for _, direction := range []search.Direction{search.Ascending, search.Descending} {
			var name string
			d, def := redisFixture(t, func([]string) any {
				r := []any{int64(3)}
				for _, id := range []string{"two", "one", "three"} {
					rank := 1
					if id == "three" {
						rank = 2
					}
					r = append(r, redisHit(name, id, "1", `{"title":"red","active":true,"rank":`+strconv.Itoa(rank)+`,"code":"a"}`)...)
				}
				return r
			})
			name = d.name
			for _, offset := range []uint64{0, 1, 10} {
				r, err := d.Search(context.Background(), def, search.Query{Text: text, Fields: []search.FieldName{"title"}, Filter: search.Exists("code"), Sort: []search.Sort{{Field: "rank", Direction: direction}}, Limit: 10, Offset: offset})
				if err != nil {
					t.Fatal(err)
				}
				if offset == 10 && len(r.Hits) != 0 {
					t.Fatal(r)
				}
				if offset == 0 {
					want := "one"
					if direction == search.Descending {
						want = "three"
					}
					if r.Hits[0].Document.ID != search.DocumentID(want) {
						t.Fatal(r)
					}
				}
			}
		}
	}
	d, def := redisFixture(t, func([]string) any { t.Error("empty literal query used the network"); return nil })
	if r, err := d.Search(context.Background(), def, search.Query{Text: "!!!"}); err != nil || r.Total.Value != 0 {
		t.Fatal(r, err)
	}
}

// TestMutationProtocol verifies atomic command acknowledgement, ordered bulk outcomes, and failure redaction.
func TestMutationProtocol(t *testing.T) {
	for _, operation := range []string{"upsert", "delete", "bulk"} {
		for _, failure := range []string{"", "info", "eval", "count", "type"} {
			t.Run(operation+"/"+failure, func(t *testing.T) {
				d, def := redisFixture(t, func(args []string) any {
					if args[0] == "FT.INFO" {
						if failure == "info" {
							return nativeError("Unknown Index name private")
						}
						return []any{}
					}
					if args[0] != "EVAL" {
						t.Error(args[0])
					}
					if failure == "eval" {
						return nativeError("private script error")
					}
					if failure == "count" {
						return int64(999)
					}
					if failure == "type" {
						return "wrong"
					}
					count, _ := strconv.Atoi(args[2])
					return int64(count - 1)
				})
				doc := search.Document{ID: "one", Body: []byte(`{"title":"red","tags":["a","b"],"active":true,"rank":1,"code":"a"}`)}
				var receipt search.Receipt
				var err error
				switch operation {
				case "upsert":
					receipt, err = d.Upsert(context.Background(), def, doc)
				case "delete":
					receipt, err = d.Delete(context.Background(), def, "one")
				case "bulk":
					upsert, e := search.UpsertMutation(doc)
					if e != nil {
						t.Fatal(e)
					}
					del, e := search.DeleteMutation("two")
					if e != nil {
						t.Fatal(e)
					}
					r, e := d.Bulk(context.Background(), def, []search.Mutation{upsert, del})
					receipt, err = r.Receipt, e
					if e == nil && (len(r.Items) != 2 || r.Items[1].ID != "two") {
						t.Fatal(r)
					}
				}
				if failure != "" {
					want := search.ErrOutcomeUnknown
					if failure == "info" {
						want = search.ErrIndexNotFound
					}
					if !errors.Is(err, want) {
						t.Fatal(err)
					}
					return
				}
				outcome, err := d.Wait(context.Background(), receipt)
				if err != nil || outcome.State != search.OutcomeSucceeded {
					t.Fatal(outcome, err)
				}
				if operation == "bulk" && (outcome.Bulk == nil || len(outcome.Bulk.Items) != 2) {
					t.Fatal(outcome)
				}
			})
		}
	}
}

// TestSchemaProtocol checks provisioning, metadata ownership, and each native command's failure.
func TestSchemaProtocol(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for failAt := 0; failAt <= 7; failAt++ {
			t.Run(fmt.Sprintf("existing-%v/failure-%d", existing, failAt), func(t *testing.T) {
				failure := failAt
				calls := 0
				metadata := ""
				exists := existing
				info := []any{"index_definition", []any{"key_type", "HASH"}, "attributes", []any{}, "index_options", []any{}}
				layout, err := schema(info)
				if err != nil {
					t.Fatal(err)
				}
				d, def := redisFixture(t, func(args []string) any {
					calls++
					if calls == failure {
						return nativeError("private failure")
					}
					switch args[0] {
					case "FT.INFO":
						if !exists {
							return nativeError("Unknown Index name")
						}
						return info
					case "FT.CREATE":
						exists = true
						return "OK"
					case "SET":
						metadata = args[2]
						return "OK"
					case "GET":
						return metadata
					default:
						t.Error(args[0])
						return nil
					}
				})
				if existing {
					b, _ := json.Marshal(map[string]string{"fingerprint": d.fingerprint, "layout": layout})
					metadata = string(b)
				}
				err = d.CreateIndex(context.Background(), def)
				if failAt > 0 && failAt <= calls {
					if err == nil || strings.Contains(err.Error(), "private") {
						t.Fatal(err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				failure = 0
				if err := d.ValidateIndex(context.Background(), def); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	for _, value := range []any{nil, []any{"attributes", []any{}}, []any{"index_definition", make(chan int), "attributes", []any{}, "index_options", []any{}}} {
		if _, err := schema(value); !errors.Is(err, search.ErrUnavailable) {
			t.Fatal(err)
		}
	}
}

// TestSchemaDriftAndLifecycle rejects missing metadata, invalid declarations, and inactive handles before I/O.
func TestSchemaDriftAndLifecycle(t *testing.T) {
	for _, scenario := range []string{"bad-info", "missing-meta", "invalid-meta", "drift"} {
		t.Run(scenario, func(t *testing.T) {
			d, def := redisFixture(t, func(args []string) any {
				if args[0] == "FT.INFO" {
					if scenario == "bad-info" {
						return []any{}
					}
					return []any{"index_definition", []any{}, "attributes", []any{}, "index_options", []any{}}
				}
				switch scenario {
				case "missing-meta":
					return nil
				case "invalid-meta":
					return "invalid"
				default:
					return `{"fingerprint":"wrong","layout":"wrong"}`
				}
			})
			want := search.ErrSchemaDrift
			if scenario == "bad-info" {
				want = search.ErrUnavailable
			}
			if err := d.ValidateIndex(context.Background(), def); !errors.Is(err, want) {
				t.Fatal(err)
			}
		})
	}
	d, def := redisFixture(t, func([]string) any { t.Error("invalid lifecycle used transport"); return nil })
	if err := d.CreateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
	if err := d.ValidateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
	if err := d.ready(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
	changed := def
	changed.Name = "different"
	if err := d.ready(context.Background(), changed); !errors.Is(err, search.ErrSchemaDrift) {
		t.Fatal(err)
	}
	d.name = ""
	if err := d.ready(context.Background(), def); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.ready(ctx, def); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := d.Wait(ctx, search.Receipt{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := d.command(ctx, false, "FT.INFO"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	d.Close()
	for _, call := range []func() error{func() error { _, err := d.Upsert(context.Background(), def, search.Document{}); return err }, func() error { _, err := d.Delete(context.Background(), def, "one"); return err }, func() error { _, err := d.Bulk(context.Background(), def, nil); return err }} {
		if err := call(); !errors.Is(err, search.ErrClosed) {
			t.Fatal(err)
		}
	}
	if _, err := New(Config{Addr: " localhost:6379"}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
}

// TestCreationRejectsInvalidInfo checks the schema response immediately after native creation.
func TestCreationRejectsInvalidInfo(t *testing.T) {
	calls := 0
	d, def := redisFixture(t, func(args []string) any {
		calls++
		if calls == 1 {
			return nativeError("Unknown Index name")
		}
		if args[0] == "FT.CREATE" {
			return "OK"
		}
		return []any{}
	})
	if err := d.CreateIndex(context.Background(), def); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
}
