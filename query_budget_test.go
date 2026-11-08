package search_test

import (
	"context"
	"errors"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// TestResolvedSearchFieldsRespectBudget preserves definitions while enforcing limits after query defaults expand.
func TestResolvedSearchFieldsRespectBudget(t *testing.T) {
	definition := minimalDefinition()
	definition.Fields = append(definition.Fields, search.Field{Name: "other", Type: search.TextField, Searchable: true})
	backend := searchfake.New()
	index, err := search.NewIndex(backend, definition, search.WithLimits(search.Limits{QueryFields: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(context.Background(), search.Query{Text: "blue"}); !errors.Is(err, search.ErrInvalidQuery) {
		t.Fatal(err)
	}
	if len(backend.Calls()) != 0 {
		t.Fatal("over-budget defaults reached the backend")
	}
	backend.QueueSearch(search.Result{Total: search.Total{Relation: search.TotalExact}}, nil)
	if _, err := index.Search(context.Background(), search.Query{Text: "blue", Fields: []search.FieldName{"title"}}); err != nil {
		t.Fatal(err)
	}
}

// TestOversizedQueryCollectionsRejectBeforeCopy bounds allocations for caller-provided slices that exceed query budgets.
func TestOversizedQueryCollectionsRejectBeforeCopy(t *testing.T) {
	index, err := search.NewIndex(searchfake.New(), minimalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []search.Query{{Fields: make([]search.FieldName, 65536)}, {Sort: make([]search.Sort, 65536)}} {
		if _, err := index.Search(context.Background(), query); !errors.Is(err, search.ErrInvalidQuery) {
			t.Fatal(err)
		}
		allocations := testing.AllocsPerRun(10, func() { _, _ = index.Search(context.Background(), query) })
		if allocations != 0 {
			t.Fatalf("rejected oversized slices caused %.0f allocations", allocations)
		}
	}
}
