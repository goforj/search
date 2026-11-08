package searchfake_test

import (
	"context"
	"errors"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// TestScriptedOwnership verifies assertions and scripted responses never alias fake-owned state.
func TestScriptedOwnership(t *testing.T) {
	backend := searchfake.New()
	score := 1.0
	backend.QueueSearch(search.Result{Hits: []search.Hit{{Document: search.Document{ID: "one", Body: []byte(`{"title":"red"}`)}, Score: &score, Highlights: map[search.FieldName][]search.Fragment{"title": {{Segments: []search.Segment{{Text: "red", Matched: true}}}}}}}, Total: search.Total{Value: 1, Relation: search.TotalExact}}, nil)
	d := search.Definition{Name: "items", Fields: []search.Field{{Name: "title", Type: search.TextField, Searchable: true}}}
	index, err := search.NewIndex(backend, d)
	if err != nil {
		t.Fatal(err)
	}
	result, err := index.Search(context.Background(), search.Query{Text: "red", Highlight: []search.Highlight{{Field: "title"}}})
	if err != nil {
		t.Fatal(err)
	}
	*result.Hits[0].Score = 9
	calls := backend.Calls()
	calls[0].Definition.Fields[0].Name = "changed"
	calls[0].Query.Highlight[0].Field = "changed"
	if backend.Calls()[0].Definition.Fields[0].Name != "title" || backend.Calls()[0].Query.Highlight[0].Field != "title" {
		t.Fatal("assertions alias fake state")
	}
	if _, err := index.Search(context.Background(), search.Query{}); err != search.ErrUnavailable {
		t.Fatal(err)
	}
}

// TestCancellationStopsEveryOperation verifies direct backend use respects cancellation before recording calls.
func TestCancellationStopsEveryOperation(t *testing.T) {
	b := searchfake.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := search.Definition{}
	checks := []func() error{
		func() error { _, err := b.Search(ctx, d, search.Query{}); return err },
		func() error { _, err := b.Upsert(ctx, d, search.Document{}); return err },
		func() error { _, err := b.Delete(ctx, d, "one"); return err },
		func() error { _, err := b.Bulk(ctx, d, nil); return err },
	}
	for _, call := range checks {
		if err := call(); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	if len(b.Calls()) != 0 {
		t.Fatal("cancelled calls were recorded")
	}
}

// TestFacetOwnership protects both queued and returned bucket slices from caller mutation.
func TestFacetOwnership(t *testing.T) {
	b := searchfake.New()
	v, err := search.Keyword("original")
	if err != nil {
		t.Fatal(err)
	}
	r := search.Result{Facets: []search.FacetResult{{Name: "tags", Buckets: []search.Bucket{{Value: v, Count: 3}}}}}
	b.QueueSearch(r, nil)
	r.Facets[0].Buckets[0].Count = 99
	got, err := b.Search(context.Background(), search.Definition{}, search.Query{})
	if err != nil || got.Facets[0].Buckets[0].Count != 3 {
		t.Fatal(got, err)
	}
}
