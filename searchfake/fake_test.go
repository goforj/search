package searchfake_test

import (
	"context"
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
