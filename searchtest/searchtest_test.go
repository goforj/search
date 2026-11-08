package searchtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/searchfake"
)

// harnessAbort models testing.T.Fatal without failing the outer assertion test.
type harnessAbort struct{}

// harnessRecorder captures expected contract failures while preserving subtest and cleanup ordering.
type harnessRecorder struct {
	mu         sync.Mutex
	scenario   string
	target     string
	fault      string
	failures   []string
	cleanups   []func()
	injections int
}

// fail retains diagnostics for the outer test to inspect.
func (r *harnessRecorder) fail(args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures = append(r.failures, fmt.Sprint(args...))
}

// selected injects a fault only within the intended scenario and operation boundary.
func (r *harnessRecorder) selected(fault string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.scenario == r.target && r.fault == fault {
		r.injections++
		return true
	}
	return false
}

// name snapshots the currently executing scenario for deterministic response fixtures.
func (r *harnessRecorder) name() string { r.mu.Lock(); defer r.mu.Unlock(); return r.scenario }

// reporter supplies a complete test reporter with fatal failures isolated to their subtest.
func (r *harnessRecorder) reporter() backendTest {
	return backendTest{
		Helper:  func() {},
		Cleanup: func(f func()) { r.cleanups = append(r.cleanups, f) },
		Error:   r.fail,
		Errorf:  func(format string, args ...any) { r.fail(fmt.Sprintf(format, args...)) },
		Fatal:   func(args ...any) { r.fail(args...); panic(harnessAbort{}) },
		Fatalf:  func(format string, args ...any) { r.fail(fmt.Sprintf(format, args...)); panic(harnessAbort{}) },
		Run: func(name string, test func(backendTest)) (ok bool) {
			r.mu.Lock()
			previous := r.scenario
			r.scenario = name
			r.mu.Unlock()
			defer func() {
				r.mu.Lock()
				r.scenario = previous
				r.mu.Unlock()
				if value := recover(); value != nil {
					if _, expected := value.(harnessAbort); !expected {
						panic(value)
					}
					ok = false
				}
			}()
			test(r.reporter())
			return true
		},
	}
}

// harnessIndex scripts expected protocol evidence solely to test whether contract assertions detect corruption.
type harnessIndex struct {
	backend    *searchfake.Backend
	recorder   *harnessRecorder
	definition search.Definition
	mu         sync.Mutex
	last       string
}

// Search provides independent expected scenario results, with targeted malformed evidence for assertion tests.
func (i *harnessIndex) Search(ctx context.Context, q search.Query) (search.Result, error) {
	r := i.recorder
	if r.selected("search-error") {
		return search.Result{}, errors.New("expected search failure")
	}
	if err := ctx.Err(); err != nil {
		return search.Result{}, err
	}
	name := r.name()
	expected := map[string][]string{
		"all": {"A", "a", "é", "中"}, "literal-all-terms": {"A", "a"}, "reversed-terms": {"A", "a"}, "empty-and": {"A", "a", "é", "中"}, "equal": {"A", "a"}, "missing-null": {"é", "中"}, "not-equal-missing": {"é", "中"}, "exists-empty-array": {"A", "a"}, "array-all": {"A"}, "array-any": {"A"}, "not-array-all": {"a", "é", "中"}, "integer-boundary": {"A"}, "less-or-equal": {"A", "a", "中"}, "greater-than": {"é"}, "greater-or-equal": {"a", "é", "中"}, "boolean-equality": {"a"}, "scalar-set": {"a", "é", "中"}, "nested-negation": {"é"}, "field-primary-text": {"é", "a", "A"}, "between": {"a", "é", "中"}, "two-sorts": {"A", "中", "a", "é"}, "utf8-keyword-order": {"a", "é", "中", "A"}, "page": {"a", "é"}, "facet-before-pagination": {"A"}, "safe-highlights": {"A"}, "replace-and-delete": {"a"}, "facet-truncation-ties": {"facet0000"}, "keyword-only-nested-index": {"nested"},
	}
	ids := expected[name]
	result := search.Result{Total: search.Total{Value: uint64(len(ids)), Relation: search.TotalExact}}
	if name == "window-edge" || name == "page" || name == "facet-before-pagination" {
		result.Total.Value = 4
	}
	if r.selected("total") {
		result.Total.Value = 999
	}
	for _, id := range ids {
		body := json.RawMessage(`{"title":"red","active":true,"rank":0,"code":"x"}`)
		if name == "keyword-only-nested-index" {
			body = []byte(`{"nested":{"a/b":[{"~key":"one"}]}}`)
		}
		if r.selected("body") {
			body = []byte("invalid")
		}
		hit := search.Hit{Document: search.Document{ID: search.DocumentID(id), Body: body}}
		for range q.Sort {
			v, _ := search.Integer(0)
			hit.SortValues = append(hit.SortValues, v)
		}
		if r.selected("sort") {
			hit.SortValues = append(hit.SortValues, search.Boolean(true))
		}
		if r.selected("ids") {
			hit.Document.ID = "wrong"
		}
		if name == "safe-highlights" {
			matched := !r.selected("highlight")
			hit.Highlights = map[search.FieldName][]search.Fragment{"title": {{Segments: []search.Segment{{Text: "red", Matched: matched}}}}}
		}
		result.Hits = append(result.Hits, hit)
	}
	for _, facet := range q.Facets {
		out := search.FacetResult{Name: facet.Name, Field: facet.Field, Precision: search.CountExact}
		values := []string{"one", "two"}
		if name == "literal-punctuation-facets" {
			values = nil
		}
		if name == "facet-truncation-ties" {
			out.Truncated = true
			values = []string{"tag0000", "tag0001"}
		}
		if name == "keyword-only-nested-index" {
			values = []string{"one"}
		}
		for _, text := range values {
			v, _ := search.Keyword(text)
			out.Buckets = append(out.Buckets, search.Bucket{Value: v, Count: 1})
		}
		if r.selected("facet-count") {
			out.Buckets[0].Count = 2
		}
		if r.selected("facet-order") {
			out.Buckets[0].Value = search.Boolean(true)
		}
		if r.selected("truncated") {
			out.Truncated = false
		}
		result.Facets = append(result.Facets, out)
	}
	if r.selected("facets") {
		result.Facets = nil
	}
	return result, nil
}

// Upsert returns real fake-backend receipt evidence unless a targeted submission failure is requested.
func (i *harnessIndex) Upsert(ctx context.Context, d search.Document) (search.Receipt, error) {
	if i.recorder.selected("upsert-error") {
		return search.Receipt{}, errors.New("expected upsert failure")
	}
	i.mu.Lock()
	i.last = "upsert"
	i.mu.Unlock()
	return i.backend.Upsert(ctx, i.definition, d)
}

// Delete isolates removal failures from the preceding replacement in the same scenario.
func (i *harnessIndex) Delete(ctx context.Context, id search.DocumentID) (search.Receipt, error) {
	if i.recorder.selected("delete-error") {
		return search.Receipt{}, errors.New("expected delete failure")
	}
	i.mu.Lock()
	i.last = "delete"
	i.mu.Unlock()
	return i.backend.Delete(ctx, i.definition, id)
}

// Bulk models per-item acceptance while allowing the harness's response-shape assertions to be tested.
func (i *harnessIndex) Bulk(ctx context.Context, ms []search.Mutation) (search.BulkResult, error) {
	if i.recorder.selected("bulk-error") {
		return search.BulkResult{}, errors.New("expected bulk failure")
	}
	i.mu.Lock()
	i.last = "bulk"
	i.mu.Unlock()
	result, err := i.backend.Bulk(ctx, i.definition, ms)
	if i.recorder.selected("bulk-items") {
		result.Items = nil
	}
	return result, err
}

// Wait exposes contradictory terminal evidence only to the contract assertion unit tests.
func (i *harnessIndex) Wait(ctx context.Context, receipt search.Receipt) (search.Outcome, error) {
	r := i.recorder
	i.mu.Lock()
	last := i.last
	i.mu.Unlock()
	if r.selected("wait-error") || last == "delete" && r.selected("wait-delete-error") {
		return search.Outcome{}, errors.New("expected inspection failure")
	}
	outcome, err := i.backend.Wait(ctx, receipt)
	if r.selected("wait-state") || last == "delete" && r.selected("wait-delete-state") {
		outcome.State = search.OutcomeFailed
	}
	if r.selected("bulk-outcome") {
		outcome.Bulk = nil
	}
	if r.selected("bulk-order") {
		outcome.Bulk.Items[0].ID = "wrong"
	}
	if r.selected("batch-precision") {
		outcome.Bulk.Precision = search.BatchOnly
	}
	return outcome, err
}

// executeHarness runs success or expected-failure fixtures without reporting their deliberate errors to testing.T.
func executeHarness(target, fault string) *harnessRecorder {
	r := &harnessRecorder{target: target, fault: fault}
	factories := portableFactories()
	factories.Keyword = func(s string) (search.Value, error) {
		if r.selected("keyword-factory") {
			return search.Value{}, errors.New("expected keyword construction failure")
		}
		return search.Keyword(s)
	}
	factories.Integer = func(n int64) (search.Value, error) {
		if r.selected("integer-factory") {
			return search.Value{}, errors.New("expected integer construction failure")
		}
		return search.Integer(n)
	}
	factories.UpsertMutation = func(d search.Document) (search.Mutation, error) {
		if r.selected("upsert-factory") {
			return search.Mutation{}, errors.New("expected mutation construction failure")
		}
		return search.UpsertMutation(d)
	}
	factories.DeleteMutation = func(id search.DocumentID) (search.Mutation, error) {
		if r.selected("delete-factory") {
			return search.Mutation{}, errors.New("expected deletion construction failure")
		}
		return search.DeleteMutation(id)
	}
	provision := func(_ context.Context, def search.Definition) (backendIndex, func(context.Context) error, error) {
		if r.selected("provision-error") {
			return nil, nil, errors.New("expected provisioning failure")
		}
		index := &harnessIndex{backend: searchfake.New(), recorder: r, definition: def}
		cleanup := func(context.Context) error {
			if r.selected("cleanup-error") {
				return errors.New("expected cleanup failure")
			}
			return nil
		}
		return index, cleanup, nil
	}
	func() {
		defer func() {
			if value := recover(); value != nil {
				if _, expected := value.(harnessAbort); !expected {
					panic(value)
				}
			}
		}()
		runBackend(r.reporter(), provision, factories)
	}()
	for n := len(r.cleanups) - 1; n >= 0; n-- {
		r.cleanups[n]()
	}
	return r
}

// TestHarnessAssertions verifies that every contract assertion actually rejects a deliberately faulty implementation.
func TestHarnessAssertions(t *testing.T) {
	baseline := executeHarness("", "no-fault")
	if len(baseline.failures) != 0 {
		t.Fatal("invalid assertion fixture", baseline.failures)
	}
	cases := map[string][]string{
		"":                          {"provision-error", "cleanup-error", "upsert-error", "wait-error", "wait-state", "keyword-factory", "integer-factory"},
		"empty-provisioned-text":    {"search-error"},
		"all":                       {"search-error", "total", "body", "sort", "ids"},
		"facet-before-pagination":   {"facets", "facet-count"},
		"safe-highlights":           {"search-error", "highlight"},
		"replace-and-delete":        {"upsert-error", "wait-error", "wait-state", "search-error", "total", "delete-error", "wait-delete-error", "wait-delete-state"},
		"bulk-order":                {"upsert-factory", "bulk-error", "bulk-items", "wait-error", "wait-state", "bulk-outcome", "bulk-order"},
		"canceled":                  {"search-error"},
		"mixed-bulk":                {"delete-factory", "upsert-factory", "bulk-error", "wait-error", "wait-state", "bulk-outcome", "batch-precision"},
		"concurrent-calls":          {"upsert-error", "wait-error", "wait-state", "search-error"},
		"facet-truncation-ties":     {"provision-error", "cleanup-error", "upsert-factory", "bulk-error", "wait-error", "wait-state", "search-error", "facets", "truncated", "facet-order"},
		"keyword-only-nested-index": {"provision-error", "cleanup-error", "upsert-error", "wait-error", "wait-state", "keyword-factory", "search-error", "total", "facets"},
	}
	for scenario, faults := range cases {
		for _, fault := range faults {
			t.Run(scenario+"/"+fault, func(t *testing.T) {
				r := executeHarness(scenario, fault)
				if r.injections == 0 || len(r.failures) == 0 {
					t.Fatal("contract failed to reject injected fault", scenario, fault, r.injections, r.failures)
				}
			})
		}
	}
}
