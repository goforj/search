package driverutil

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/goforj/search"
)

// TestFacetOrderingAndTruncation verifies stable ties, count order, and corrupt stored scalars.
func TestFacetOrderingAndTruncation(t *testing.T) {
	d := search.Definition{Fields: []search.Field{{Name: "tag", Type: search.KeywordField}}}
	f := search.TermFacet{Name: "tags", Field: "tag", Limit: 2}
	r, err := Facet(f, d, map[string]uint64{"v61": 2, "v62": 2, "v63": 3}, search.CountExact)
	if err != nil || !r.Truncated || len(r.Buckets) != 2 || r.Buckets[0].Count != 3 {
		t.Fatal(r, err)
	}
	for n, want := range []string{"c", "a"} {
		got, ok := r.Buckets[n].Value.Keyword()
		if !ok || got != want {
			t.Fatal(got, want)
		}
	}
	if _, err := Facet(f, d, map[string]uint64{"corrupt": 1}, search.CountExact); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	empty := EmptyResult(search.Query{Facets: []search.TermFacet{f}})
	if empty.Total.Relation != search.TotalExact || len(empty.Facets) != 1 || empty.Facets[0].Name != f.Name || empty.Facets[0].Precision != search.CountExact {
		t.Fatal(empty)
	}
}

// TestScalarAndProjectionBoundaries checks empty scalars, nested arrays, and malformed persisted types.
func TestScalarAndProjectionBoundaries(t *testing.T) {
	if Scalar(search.Value{}) != "" || NativeValue(nil, search.TextField).Kind() != "" {
		t.Fatal("unsupported scalar was represented as a valid value")
	}
	for _, tc := range []struct {
		encoded string
		kind    search.FieldType
	}{{"bad", search.KeywordField}, {"v0", search.KeywordField}, {"2", search.BooleanField}, {"1", search.IntegerField}} {
		if _, err := DecodeScalar(tc.encoded, tc.kind); !errors.Is(err, search.ErrUnavailable) {
			t.Fatal(tc, err)
		}
	}
	doc := search.Document{ID: "one", Body: []byte(`{"a":[{"title":" red, BLUE!"}],"tags":["a","b"],"active":false,"rank":1}`)}
	obj := Object(doc)
	for _, pointer := range []search.JSONPointer{"/a/01", "/a/-1", "/a/2", "/rank/child", "/missing"} {
		if _, ok := At(obj, pointer); ok {
			t.Fatal(pointer)
		}
	}
	d := search.Definition{Fields: []search.Field{
		{Name: "title", Path: "/a/0/title", Type: search.TextField},
		{Name: "missing", Path: "/missing", Type: search.TextField},
		{Name: "tags", Path: "/tags", Type: search.KeywordsField},
		{Name: "active", Path: "/active", Type: search.BooleanField},
		{Name: "rank", Path: "/rank", Type: search.IntegerField},
	}}
	physical := Physical(doc, d)
	if !reflect.DeepEqual(physical[FieldKey("tags")], []string{"v61", "v62"}) || physical[FieldKey("active")] != "0" {
		t.Fatal(physical)
	}
	q := search.Query{Text: "red", Highlight: []search.Highlight{{Field: "missing"}, {Field: "title"}}}
	h := Highlights(doc, d, q)
	segments := h["title"][0].Segments
	if len(segments) != 5 || segments[0].Text != " " || !segments[1].Matched || segments[3].Matched || segments[4].Text != "!" {
		t.Fatal(segments)
	}
	if _, err := Hit("one", "{", d, q, nil); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := Hit("one", string(doc.Body), d, search.Query{Sort: []search.Sort{{Field: "missing"}}}, nil); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if validStoredFields(obj, search.Definition{Fields: []search.Field{{Path: "/rank", Type: "unknown"}}}) {
		t.Fatal("unknown stored type accepted")
	}
}

// TestLedgerEvictionAndCancellation keeps retained evidence bounded and respects cancelled inspections.
func TestLedgerEvictionAndCancellation(t *testing.T) {
	l := &Ledger{}
	first, err := l.Complete(search.Outcome{State: search.OutcomeSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 4096; n++ {
		if _, err := l.Complete(search.Outcome{State: search.OutcomeSucceeded}); err != nil {
			t.Fatal(err)
		}
	}
	o, err := l.Wait(context.Background(), first)
	if err != nil || o.State != search.OutcomeUnknown || !errors.Is(o.Error, search.ErrReceiptExpired) || len(l.entries) != 4096 {
		t.Fatal(o, err, len(l.entries))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Wait(ctx, first); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
