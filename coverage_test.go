package search

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// TestDefinitionEncodingBudgets checks limits after canonical JSON expansion.
func TestDefinitionEncodingBudgets(t *testing.T) {
	if _, err := (Definition{}).Fingerprint(); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatal(err)
	}
	limits := DefaultLimits()
	limits.DefinitionBytes = 1
	if _, err := normalizeDefinition(testDefinition(), limits); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatal(err)
	}
	if validSort(make([]Sort, 3), nil) {
		t.Fatal("accepted more than two sort fields")
	}
}

// TestJSONTokenBoundaries checks decoder failures and token types outside the canonical object parser.
func TestJSONTokenBoundaries(t *testing.T) {
	for _, input := range []string{"", "1.5", `{"unterminated`, `{"bad\uXXXX":0}`} {
		decoder := json.NewDecoder(strings.NewReader(input))
		members := 0
		if _, err := readJSON(decoder, 0, &members, DefaultLimits()); err == nil {
			t.Fatal("accepted noncanonical token", input)
		}
	}
	decoder := json.NewDecoder(strings.NewReader("[]"))
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	members := 0
	if _, err := readJSON(decoder, 0, &members, DefaultLimits()); !errors.Is(err, ErrInvalidDocument) {
		t.Fatal(err)
	}
}

// concurrentWaitBackend releases two admitted inspections in an explicitly controlled order.
type concurrentWaitBackend struct {
	Backend
	calls   atomic.Int32
	started chan int
	gates   [2]chan struct{}
}

// Wait permits a second response to arrive after the first response has populated the cache.
func (b *concurrentWaitBackend) Wait(context.Context, Receipt) (Outcome, error) {
	n := int(b.calls.Add(1)) - 1
	b.started <- n
	<-b.gates[n]
	return Outcome{State: OutcomeSucceeded}, nil
}

// TestConcurrentWaitKeepsFirstTerminalEvidence proves the second admitted waiter uses the shared cached outcome.
func TestConcurrentWaitKeepsFirstTerminalEvidence(t *testing.T) {
	b := &concurrentWaitBackend{started: make(chan int, 2), gates: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
	i, err := NewIndex(b, testDefinition())
	if err != nil {
		t.Fatal(err)
	}
	r, err := i.bind(Receipt{token: []byte("accepted")})
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan Outcome, 2)
	for n := 0; n < 2; n++ {
		go func() {
			o, err := i.Wait(context.Background(), r)
			if err != nil {
				t.Error(err)
			}
			results <- o
		}()
	}
	<-b.started
	<-b.started
	close(b.gates[0])
	first := <-results
	close(b.gates[1])
	second := <-results
	if first.VisibleAt.IsZero() || first.VisibleAt != second.VisibleAt {
		t.Fatal("waiters observed different terminal evidence", first, second)
	}
}

// TestDocumentEscapesAndTypedRoundTrip checks truncation and canonical expansion independently of JSON syntax.
func TestDocumentEscapesAndTypedRoundTrip(t *testing.T) {
	for _, body := range []string{`"\`, `"\u12`, `"\uXXXX"`, `"\ud800\uXXXX"`, `"\ud800\u0000"`} {
		if validUnicodeEscapes([]byte(body)) {
			t.Errorf("accepted invalid escape %q", body)
		}
	}
	if _, ok := fieldValue(map[string]any{}, "invalid"); ok {
		t.Fatal("accepted malformed pointer")
	}
	doc, err := Encode("one", map[string]string{"title": "red"})
	if err != nil {
		t.Fatal(err)
	}
	value, err := Decode[map[string]string](doc)
	if err != nil || value["title"] != "red" {
		t.Fatal(value, err)
	}
	limits := DefaultLimits()
	limits.DocumentBytes = len(`{"x":"<"}`)
	if _, _, err := normalizeDocument(Document{ID: "one", Body: []byte(`{"x":"<"}`)}, Definition{}, limits); !errors.Is(err, ErrInvalidDocument) {
		t.Fatal("canonical expansion exceeded budget", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Upsert(ctx, nil, "one", struct{}{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	index, err := NewIndex(&boundaryBackend{}, testDefinition())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SearchAs[struct{}](ctx, index, Query{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestFilterInternalValidation rejects malformed union shapes before a driver sees them.
func TestFilterInternalValidation(t *testing.T) {
	d, err := normalizeDefinition(testDefinition(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []Filter{
		{node: &filterNode{kind: FilterEqual, field: "code", values: []Value{{kind: KeywordValue}, {kind: KeywordValue}}}},
		{node: &filterNode{kind: "invalid", field: "code", values: []Value{{kind: KeywordValue}}}},
	} {
		if _, err := normalizeQuery(Query{Filter: filter}, d, DefaultLimits()); !errors.Is(err, ErrInvalidQuery) {
			t.Fatal(err)
		}
	}
	fields := map[FieldName]Field{"text": {Name: "text", Type: TextField, Filterable: true}}
	nodes, budget := 0, 0
	if validateFilter(Equal("text", Boolean(true)), fields, DefaultLimits(), 1, &nodes, &budget) {
		t.Fatal("accepted unsupported field shape")
	}
	limits := DefaultLimits()
	limits.QueryBytes = 1
	if _, err := normalizeQuery(Query{Filter: Exists("code")}, d, limits); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
	limits.QueryBytes = 20
	if _, err := normalizeQuery(Query{Text: strings.Repeat("a", 20)}, d, limits); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
}

// boundaryBackend supplies intentionally corrupt responses to exercise the consumer boundary.
type boundaryBackend struct {
	Backend
	result Result
	bulk   BulkResult
}

// Search returns controlled stored-document corruption without involving a real engine.
func (b *boundaryBackend) Search(context.Context, Definition, Query) (Result, error) {
	return b.result, nil
}

// Bulk returns controlled protocol evidence to verify submission integrity checks.
func (b *boundaryBackend) Bulk(context.Context, Definition, []Mutation) (BulkResult, error) {
	return b.bulk, nil
}

// Wait models an incomplete terminal response that omits accepted bulk evidence.
func (b *boundaryBackend) Wait(context.Context, Receipt) (Outcome, error) {
	return Outcome{State: OutcomeSucceeded}, nil
}

// TestIndexRejectsCorruptResponses ensures malformed evidence never becomes successful public output.
func TestIndexRejectsCorruptResponses(t *testing.T) {
	b := &boundaryBackend{}
	var observed Event
	i, err := NewIndex(b, testDefinition(), WithObserver(ObserverFunc(func(_ context.Context, e Event) { observed = e })))
	if err != nil {
		t.Fatal(err)
	}
	for _, hits := range [][]Hit{
		{{Document: Document{ID: "one", Body: json.RawMessage(`[]`)}}},
		{{Document: Document{ID: "one", Body: json.RawMessage(`{"code":"a","price":1}`)}}, {Document: Document{ID: "two", Body: json.RawMessage(`{"code":"b","price":2}`)}}},
	} {
		b.result.Hits = hits
		if _, err := i.Search(context.Background(), Query{Limit: 1}); !errors.Is(err, ErrUnavailable) || observed.ErrorCode != ErrUnavailable.Error() {
			t.Fatal(err, observed)
		}
	}
	for _, mutations := range [][]Mutation{
		{{kind: MutationUpsert, doc: Document{ID: "one", Body: []byte(`[]`)}}},
		{{kind: "invalid", doc: Document{ID: "one"}}},
	} {
		if _, err := i.Bulk(context.Background(), mutations); !errors.Is(err, ErrInvalidDocument) {
			t.Fatal(err)
		}
	}
	m, err := DeleteMutation("one")
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReceipt([]byte("valid"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bulk := range []BulkResult{
		{Receipt: r},
		{Receipt: r, Precision: PerItem, Items: []ItemResult{{Position: 1, ID: "one", Status: ItemAccepted}}},
		{Receipt: r, Precision: PerItem, Items: []ItemResult{{Position: 0, ID: "one", Status: ItemSucceeded}}},
	} {
		b.bulk = bulk
		if _, err := i.Bulk(context.Background(), []Mutation{m}); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	if _, err := i.bind(Receipt{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := i.Wait(context.Background(), Receipt{owner: i, token: []byte("unbound")}); !errors.Is(err, ErrInvalidReceipt) {
		t.Fatal(err)
	}
	bound, err := i.bind(r)
	if err != nil {
		t.Fatal(err)
	}
	bound.state.bulk = []ItemResult{{ID: "one", Status: ItemAccepted}}
	if _, err := i.Wait(context.Background(), bound); !errors.Is(err, ErrUnavailable) {
		t.Fatal("incomplete bulk outcome accepted", err)
	}
	unknown := SafeError(errors.Join(NewUnknownOutcomeError(r), context.Canceled, errors.New("private")))
	if !errors.Is(unknown, ErrOutcomeUnknown) || !errors.Is(unknown, context.Canceled) || strings.Contains(unknown.Error(), "private") {
		t.Fatal(unknown)
	}
}

// TestDefinitionPointerBudgetRejectsBeforeCopy prevents oversized schema paths from forcing JSON encoding allocations.
func TestDefinitionPointerBudgetRejectsBeforeCopy(t *testing.T) {
	for _, test := range []struct {
		name          string
		fields, bytes int
	}{
		{"single-pointer", 1, 1 << 20},
		{"aggregate-pointers", 3, 32 << 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := testDefinition()
			for n := 0; n < test.fields; n++ {
				definition.Fields[n].Path = JSONPointer("/" + strings.Repeat("x", test.bytes))
			}
			limits := DefaultLimits()
			if _, err := normalizeDefinition(definition, limits); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatal(err)
			}
			if allocations := testing.AllocsPerRun(10, func() { _, _ = normalizeDefinition(definition, limits) }); allocations != 0 {
				t.Fatalf("rejected oversized schema paths caused %.0f allocations", allocations)
			}
		})
	}
}
