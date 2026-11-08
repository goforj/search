// Package searchtest supplies reusable semantic fixtures for real backend drivers.
package searchtest

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/goforj/search"
)

// Provision creates an isolated, provisioned index and an idempotent cleanup callback.
type Provision func(context.Context, search.Definition) (*search.Index, func(context.Context) error, error)

// Definition returns the shared corpus schema with nullable, array, and required sort fields.
// @group Testing
func Definition() search.Definition {
	return search.Definition{Name: "contract", Fields: []search.Field{
		{Name: "title", Type: search.TextField, Searchable: true},
		{Name: "category", Type: search.KeywordField, Filterable: true, Facetable: true},
		{Name: "tags", Type: search.KeywordsField, Filterable: true, Facetable: true},
		{Name: "active", Type: search.BooleanField, Filterable: true, Facetable: true, Sortable: true, Required: true},
		{Name: "rank", Type: search.IntegerField, Filterable: true, Sortable: true, Required: true},
		{Name: "code", Type: search.KeywordField, Filterable: true, Sortable: true, Required: true},
	}}
}

// RunBackend proves one portable corpus against an actual provisioned driver.
// @group Testing
func RunBackend(t *testing.T, provision Provision) {
	t.Helper()
	runBackend(backendReporter(t), func(ctx context.Context, def search.Definition) (backendIndex, func(context.Context) error, error) {
		index, cleanup, err := provision(ctx, def)
		return index, cleanup, err
	}, portableFactories())
}

// backendIndex limits the harness to the public request operations it verifies.
type backendIndex interface {
	Search(context.Context, search.Query) (search.Result, error)
	Upsert(context.Context, search.Document) (search.Receipt, error)
	Delete(context.Context, search.DocumentID) (search.Receipt, error)
	Bulk(context.Context, []search.Mutation) (search.BulkResult, error)
	Wait(context.Context, search.Receipt) (search.Outcome, error)
}

// backendProvision isolates harness assertion tests from the domain handle's own validation.
type backendProvision func(context.Context, search.Definition) (backendIndex, func(context.Context) error, error)

// backendTest binds test reporting so the contract's own failure assertions can be verified in isolation.
type backendTest struct {
	Helper  func()
	Cleanup func(func())
	Error   func(...any)
	Errorf  func(string, ...any)
	Fatal   func(...any)
	Fatalf  func(string, ...any)
	Run     func(string, func(backendTest)) bool
}

// backendReporter preserves testing.T cleanup, subtest, and failure semantics for public contract runs.
func backendReporter(t *testing.T) backendTest {
	return backendTest{
		Helper: t.Helper, Cleanup: t.Cleanup, Error: t.Error, Errorf: t.Errorf, Fatal: t.Fatal, Fatalf: t.Fatalf,
		Run: func(name string, test func(backendTest)) bool {
			return t.Run(name, func(child *testing.T) { test(backendReporter(child)) })
		},
	}
}

// backendFactories keeps fixture construction wired to the same public constructors consumers call.
type backendFactories struct {
	Keyword        func(string) (search.Value, error)
	Integer        func(int64) (search.Value, error)
	UpsertMutation func(search.Document) (search.Mutation, error)
	DeleteMutation func(search.DocumentID) (search.Mutation, error)
}

// portableFactories binds fixture values and mutations without optional or global dependencies.
func portableFactories() backendFactories {
	return backendFactories{search.Keyword, search.Integer, search.UpsertMutation, search.DeleteMutation}
}

// runBackend executes the contract through independently testable reporting and fixture construction.
func runBackend(t backendTest, provision backendProvision, factories backendFactories) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	seen := map[string]bool{}
	run := func(name string, test func(backendTest)) bool { seen[name] = true; return t.Run(name, test) }
	defer func() {
		for _, name := range ScenarioNames() {
			if !seen[name] {
				t.Errorf("contract scenario not executed: %s", name)
			}
		}
		if len(seen) != len(ScenarioNames()) {
			t.Error("scenario inventory does not match executed fixtures")
		}
	}()
	idx, close, err := provision(ctx, Definition())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := close(cleanup); err != nil {
			t.Error(err)
		}
	})
	run("empty-provisioned-text", func(t backendTest) {
		result, err := idx.Search(ctx, search.Query{Text: "anything"})
		if err != nil || result.Total.Value != 0 || len(result.Hits) != 0 {
			t.Fatal(result, err)
		}
	})
	corpus := []struct {
		id   string
		body string
	}{
		{"A", `{"title":"red blue <script>","category":"fruit","tags":["one","one","two"],"active":true,"rank":-9007199254740991,"code":"É"}`},
		{"a", `{"title":"blue red","category":"fruit","tags":[],"active":false,"rank":0,"code":"Z"}`},
		{"é", `{"title":"red","category":null,"active":true,"rank":9007199254740991,"code":"a"}`},
		{"中", `{"title":"blue","tags":null,"active":true,"rank":0,"code":"a"}`},
	}
	for _, doc := range corpus {
		r, e := idx.Upsert(ctx, search.Document{ID: search.DocumentID(doc.id), Body: json.RawMessage(doc.body)})
		if e != nil {
			t.Fatal(e)
		}
		o, e := idx.Wait(ctx, r)
		if e != nil || o.State != search.OutcomeSucceeded {
			t.Fatalf("upsert visibility: %+v %v", o, e)
		}
	}
	kw := func(s string) search.Value {
		v, e := factories.Keyword(s)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	integer := func(n int64) search.Value {
		v, e := factories.Integer(n)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	cases := []struct {
		name  string
		q     search.Query
		ids   []string
		total uint64
	}{
		{"all", search.Query{}, []string{"A", "a", "é", "中"}, 4},
		{"literal-all-terms", search.Query{Text: `"blue" -red`}, nil, 2},
		{"reversed-terms", search.Query{Text: "red blue"}, nil, 2},
		{"literal-punctuation-facets", search.Query{Text: "* + - :", Facets: []search.TermFacet{{Name: "tags", Field: "tags"}}}, []string{}, 0},
		{"empty-and", search.Query{Filter: search.And()}, []string{"A", "a", "é", "中"}, 4},
		{"empty-or", search.Query{Filter: search.Or()}, []string{}, 0},
		{"equal", search.Query{Filter: search.Equal("category", kw("fruit"))}, []string{"A", "a"}, 2},
		{"missing-null", search.Query{Filter: search.Not(search.Exists("category"))}, []string{"é", "中"}, 2},
		{"not-equal-missing", search.Query{Filter: search.Not(search.Equal("category", kw("fruit")))}, []string{"é", "中"}, 2},
		{"exists-empty-array", search.Query{Filter: search.Exists("tags")}, []string{"A", "a"}, 2},
		{"array-all", search.Query{Filter: search.ContainsAll("tags", kw("one"), kw("two"))}, []string{"A"}, 1},
		{"array-any", search.Query{Filter: search.ContainsAny("tags", kw("two"), kw("none"))}, []string{"A"}, 1},
		{"not-array-all", search.Query{Filter: search.Not(search.ContainsAll("tags", kw("one"), kw("two")))}, []string{"a", "é", "中"}, 3},
		{"integer-boundary", search.Query{Filter: search.LessThan("rank", integer(0))}, []string{"A"}, 1},
		{"less-or-equal", search.Query{Filter: search.LessThanOrEqual("rank", integer(0))}, []string{"A", "a", "中"}, 3},
		{"greater-than", search.Query{Filter: search.GreaterThan("rank", integer(0))}, []string{"é"}, 1},
		{"greater-or-equal", search.Query{Filter: search.GreaterThanOrEqual("rank", integer(0))}, []string{"a", "é", "中"}, 3},
		{"boolean-equality", search.Query{Filter: search.Equal("active", search.Boolean(false))}, []string{"a"}, 1},
		{"scalar-set", search.Query{Filter: search.In("code", kw("Z"), kw("a"))}, []string{"a", "é", "中"}, 3},
		{"nested-negation", search.Query{Filter: search.Not(search.Or(search.Equal("category", kw("fruit")), search.LessThan("rank", integer(1))))}, []string{"é"}, 1},
		{"field-primary-text", search.Query{Text: "red", Sort: []search.Sort{{Field: "rank", Direction: search.Descending}}}, []string{"é", "a", "A"}, 3},
		{"literal-operators", search.Query{Text: "title:red OR blue"}, []string{}, 0},
		{"window-edge", search.Query{Offset: 999, Limit: 1}, []string{}, 4},
		{"between", search.Query{Filter: search.Between("rank", integer(0), integer(search.MaxInteger))}, []string{"a", "é", "中"}, 3},
		{"two-sorts", search.Query{Sort: []search.Sort{{Field: "rank", Direction: search.Ascending}, {Field: "active", Direction: search.Descending}}}, []string{"A", "中", "a", "é"}, 4},
		{"utf8-keyword-order", search.Query{Sort: []search.Sort{{Field: "code", Direction: search.Ascending}}}, []string{"a", "é", "中", "A"}, 4},
		{"page", search.Query{Offset: 1, Limit: 2}, []string{"a", "é"}, 4},
		{"facet-before-pagination", search.Query{Limit: 1, Facets: []search.TermFacet{{Name: "tags", Field: "tags"}}}, []string{"A"}, 4},
	}
	for _, tc := range cases {
		run(tc.name, func(t backendTest) {
			r, e := idx.Search(ctx, tc.q)
			if e != nil {
				t.Fatal(e)
			}
			if r.Total.Value != tc.total {
				t.Fatalf("total=%+v want %d", r.Total, tc.total)
			}
			ids := []string{}
			for _, h := range r.Hits {
				ids = append(ids, string(h.Document.ID))
				if !json.Valid(h.Document.Body) {
					t.Fatal("invalid stored body")
				}
				if len(h.SortValues) != len(tc.q.Sort) {
					t.Fatalf("sort metadata: %+v", h.SortValues)
				}
			}
			if tc.ids != nil && !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("ids=%v want %v", ids, tc.ids)
			}
			if len(tc.q.Facets) > 0 {
				expectedBuckets := 2
				if tc.total == 0 {
					expectedBuckets = 0
				}
				if len(r.Facets) != 1 || len(r.Facets[0].Buckets) != expectedBuckets {
					t.Fatalf("facets=%+v", r.Facets)
				}
				for _, b := range r.Facets[0].Buckets {
					if b.Count != 1 {
						t.Fatal("array duplicate counted twice")
					}
				}
			}
		})
	}
	run("safe-highlights", func(t backendTest) {
		r, e := idx.Search(ctx, search.Query{Text: "red", Highlight: []search.Highlight{{Field: "title"}}})
		if e != nil {
			t.Fatal(e)
		}
		matched := false
		for _, h := range r.Hits {
			for _, f := range h.Highlights["title"] {
				for _, s := range f.Segments {
					matched = matched || s.Matched
				}
			}
		}
		if !matched {
			t.Fatal("no matched segments")
		}
	})
	run("replace-and-delete", func(t backendTest) {
		r, e := idx.Upsert(ctx, search.Document{ID: "A", Body: json.RawMessage(`{"active":true,"rank":1,"code":"x"}`)})
		if e != nil {
			t.Fatal(e)
		}
		if o, e := idx.Wait(ctx, r); e != nil || o.State != search.OutcomeSucceeded {
			t.Fatal(o, e)
		}
		found, e := idx.Search(ctx, search.Query{Text: "red blue"})
		if e != nil || found.Total.Value != 1 {
			t.Fatal(found, e)
		}
		for n := 0; n < 2; n++ {
			r, e = idx.Delete(ctx, "A")
			if e != nil {
				t.Fatal(e)
			}
			if o, e := idx.Wait(ctx, r); e != nil || o.State != search.OutcomeSucceeded {
				t.Fatal(o, e)
			}
		}
	})
	run("bulk-order", func(t backendTest) {
		ms := []search.Mutation{}
		for n := 0; n < 3; n++ {
			doc := search.Document{ID: search.DocumentID(fmt.Sprintf("bulk%d", n)), Body: json.RawMessage(`{"active":true,"rank":1,"code":"x"}`)}
			m, e := factories.UpsertMutation(doc)
			if e != nil {
				t.Fatal(e)
			}
			ms = append(ms, m)
		}
		r, e := idx.Bulk(ctx, ms)
		if e != nil {
			t.Fatal(e)
		}
		if len(r.Items) != len(ms) {
			t.Fatal(r)
		}
		o, e := idx.Wait(ctx, r.Receipt)
		if e != nil || o.State != search.OutcomeSucceeded || o.Bulk == nil {
			t.Fatal(o, e)
		}
		for n, item := range o.Bulk.Items {
			if item.Position != n || item.ID != ms[n].ID() {
				t.Fatal("bulk order lost")
			}
		}
	})
	run("canceled", func(t backendTest) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, e := idx.Search(c, search.Query{}); e != context.Canceled {
			t.Fatal(e)
		}
	})
	run("mixed-bulk", func(t backendTest) {
		deletion, err := factories.DeleteMutation("bulk0")
		if err != nil {
			t.Fatal(err)
		}
		replacement, err := factories.UpsertMutation(search.Document{ID: "mixed", Body: json.RawMessage(`{"active":true,"rank":1,"code":"x"}`)})
		if err != nil {
			t.Fatal(err)
		}
		batch, err := idx.Bulk(ctx, []search.Mutation{deletion, replacement})
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := idx.Wait(ctx, batch.Receipt)
		if err != nil || outcome.State != search.OutcomeSucceeded || outcome.Bulk == nil {
			t.Fatal(outcome, err)
		}
		if outcome.Bulk.Precision == search.BatchOnly {
			for _, item := range outcome.Bulk.Items {
				if item.Status != search.ItemUnknown {
					t.Fatal("invented batch-only per-item success")
				}
			}
		}
	})
	run("concurrent-calls", func(t backendTest) {
		var workers sync.WaitGroup
		for n := 0; n < 8; n++ {
			workers.Add(1)
			go func(n int) {
				defer workers.Done()
				doc := search.Document{ID: search.DocumentID(fmt.Sprintf("concurrent%d", n)), Body: json.RawMessage(`{"active":true,"rank":1,"code":"x"}`)}
				receipt, err := idx.Upsert(ctx, doc)
				if err != nil {
					t.Error(err)
					return
				}
				outcome, err := idx.Wait(ctx, receipt)
				if err != nil || outcome.State != search.OutcomeSucceeded {
					t.Error(outcome, err)
					return
				}
				if _, err := idx.Search(ctx, search.Query{}); err != nil {
					t.Error(err)
				}
			}(n)
		}
		workers.Wait()
	})

	run("facet-truncation-ties", func(t backendTest) {
		definition := Definition()
		definition.Name = "facets"
		index, cleanup, err := provision(ctx, definition)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := cleanup(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		for start := 0; start < 1700; start += 500 {
			mutations := []search.Mutation{}
			for n := start; n < min(start+500, 1700); n++ {
				body := fmt.Sprintf(`{"active":true,"rank":0,"code":"x","category":"tag%04d"}`, n)
				mutation, err := factories.UpsertMutation(search.Document{ID: search.DocumentID(fmt.Sprintf("facet%04d", n)), Body: json.RawMessage(body)})
				if err != nil {
					t.Fatal(err)
				}
				mutations = append(mutations, mutation)
			}
			batch, err := index.Bulk(ctx, mutations)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := index.Wait(ctx, batch.Receipt)
			if err != nil || outcome.State != search.OutcomeSucceeded {
				t.Fatal(outcome, err)
			}
		}
		result, err := index.Search(ctx, search.Query{Limit: 1, Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 2}}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Facets) != 1 || !result.Facets[0].Truncated || len(result.Facets[0].Buckets) != 2 {
			t.Fatal(result.Facets)
		}
		for n, bucket := range result.Facets[0].Buckets {
			value, ok := bucket.Value.Keyword()
			if !ok || value != fmt.Sprintf("tag%04d", n) || bucket.Count != 1 {
				t.Fatal("facet tie order or precision", result.Facets)
			}
		}
	})

	run("cross-field-all-terms", func(t backendTest) {
		definition := search.Definition{Name: "crossfields", Fields: []search.Field{
			{Name: "title", Type: search.TextField, Searchable: true},
			{Name: "summary", Type: search.TextField, Searchable: true},
		}}
		index, cleanup, err := provision(ctx, definition)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := cleanup(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		for _, document := range []search.Document{
			{ID: "split", Body: json.RawMessage(`{"title":"copper","summary":"kettle"}`)},
			{ID: "title", Body: json.RawMessage(`{"title":"copper kettle"}`)},
			{ID: "other", Body: json.RawMessage(`{"title":"copper","summary":"spoon"}`)},
		} {
			receipt, err := index.Upsert(ctx, document)
			if err != nil {
				t.Fatal(err)
			}
			if outcome, err := index.Wait(ctx, receipt); err != nil || outcome.State != search.OutcomeSucceeded {
				t.Fatal(outcome, err)
			}
		}
		for _, test := range []struct {
			fields []search.FieldName
			ids    []string
		}{
			{nil, []string{"split", "title"}},
			{[]search.FieldName{"title"}, []string{"title"}},
			{[]search.FieldName{"summary"}, []string{}},
		} {
			result, err := index.Search(ctx, search.Query{Text: "copper kettle", Fields: test.fields})
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, hit := range result.Hits {
				ids = append(ids, string(hit.Document.ID))
			}
			sort.Strings(ids)
			if result.Total.Value != uint64(len(test.ids)) || !reflect.DeepEqual(ids, test.ids) {
				t.Fatalf("fields=%v: hits=%v total=%v want=%v", test.fields, ids, result.Total, test.ids)
			}
		}
	})

	run("keyword-only-nested-index", func(t backendTest) {
		definition := search.Definition{Name: "nested", Fields: []search.Field{{Name: "code", Path: "/nested/a~1b/0/~0key", Type: search.KeywordField, Required: true, Filterable: true, Sortable: true, Facetable: true}}}
		index, cleanup, err := provision(ctx, definition)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := cleanup(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		receipt, err := index.Upsert(ctx, search.Document{ID: "nested", Body: json.RawMessage(`{"nested":{"a/b":[{"~key":"one"}]}}`)})
		if err != nil {
			t.Fatal(err)
		}
		if outcome, err := index.Wait(ctx, receipt); err != nil || outcome.State != search.OutcomeSucceeded {
			t.Fatal(outcome, err)
		}
		value, err := factories.Keyword("one")
		if err != nil {
			t.Fatal(err)
		}
		result, err := index.Search(ctx, search.Query{Filter: search.Equal("code", value), Sort: []search.Sort{{Field: "code", Direction: search.Ascending}}, Facets: []search.TermFacet{{Name: "codes", Field: "code"}}})
		if err != nil || result.Total.Value != 1 || len(result.Hits) != 1 || result.Hits[0].Document.ID != "nested" || len(result.Facets) != 1 || len(result.Facets[0].Buckets) != 1 {
			t.Fatal(result, err)
		}
	})

}

// ScenarioNames inventories required fixtures for generated driver evidence. A skipped or missing scenario is not support.
// @group Testing
func ScenarioNames() []string {
	return []string{
		"empty-provisioned-text",
		"all",
		"literal-all-terms",
		"reversed-terms",
		"literal-punctuation-facets",
		"empty-and",
		"empty-or",
		"equal",
		"missing-null",
		"not-equal-missing",
		"exists-empty-array",
		"array-all",
		"array-any",
		"not-array-all",
		"integer-boundary",
		"less-or-equal",
		"greater-than",
		"greater-or-equal",
		"boolean-equality",
		"scalar-set",
		"nested-negation",
		"field-primary-text",
		"literal-operators",
		"window-edge",
		"between",
		"two-sorts",
		"utf8-keyword-order",
		"page",
		"facet-before-pagination",
		"safe-highlights",
		"replace-and-delete",
		"bulk-order",
		"canceled",
		"mixed-bulk",
		"concurrent-calls",
		"facet-truncation-ties",
		"cross-field-all-terms",
		"keyword-only-nested-index",
	}
}
