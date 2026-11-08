package blevesearch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	blevesearch "github.com/blevesearch/bleve/v2/search"
	"github.com/goforj/search"
	"github.com/goforj/search/searchtest"
)

// embeddedIndex permits overriding the native Index method without shadowing an embedded field.
type embeddedIndex interface{ bleve.Index }

// faultyIndex preserves real batch construction while controlling native operation failures.
type faultyIndex struct {
	embeddedIndex
	fail   string
	cancel context.CancelFunc
	result *bleve.SearchResult
}

// Index simulates uncertain write acceptance independently of input validation.
func (i *faultyIndex) Index(string, any) error { return errors.New("private native write") }

// Delete simulates uncertain deletion acknowledgement.
func (i *faultyIndex) Delete(string) error { return errors.New("private native delete") }

// Batch simulates an interrupted atomic commit acknowledgement.
func (i *faultyIndex) Batch(*bleve.Batch) error { return errors.New("private native batch") }

// NewBatch can cancel admission while the driver constructs an otherwise valid batch.
func (i *faultyIndex) NewBatch() *bleve.Batch {
	if i.cancel != nil {
		i.cancel()
	}
	return i.embeddedIndex.NewBatch()
}

// SearchInContext models native search failures separately from incomplete shard results.
func (i *faultyIndex) SearchInContext(context.Context, *bleve.SearchRequest) (*bleve.SearchResult, error) {
	if i.fail == "search" {
		return nil, errors.New("private native query")
	}
	return i.result, nil
}

// SetInternal makes ownership initialization failure reproducible without corrupting an actual index.
func (i *faultyIndex) SetInternal([]byte, []byte) error { return errors.New("private metadata write") }

// GetInternal makes metadata read failure reproducible without filesystem timing.
func (i *faultyIndex) GetInternal([]byte) ([]byte, error) {
	return nil, errors.New("private metadata read")
}

// bleveFixture constructs a real memory index so fault tests retain actual mapping and batch objects.
func bleveFixture(t *testing.T) (*Driver, search.Definition, *faultyIndex) {
	t.Helper()
	ctx := context.Background()
	d, err := Open(ctx, Config{Memory: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateIndex(ctx, searchtest.Definition()); err != nil {
		t.Fatal(err)
	}
	idx, err := search.NewIndex(d, searchtest.Definition())
	if err != nil {
		t.Fatal(err)
	}
	f := &faultyIndex{embeddedIndex: d.index}
	d.index = f
	t.Cleanup(func() { d.Close() })
	return d, idx.Definition(), f
}

// TestNativeFailures preserve uncertainty and safe classifications at every native boundary.
func TestNativeFailures(t *testing.T) {
	d, def, f := bleveFixture(t)
	ctx := context.Background()
	doc := search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"a"}`)}
	if _, err := d.Upsert(ctx, def, doc); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if _, err := d.Delete(ctx, def, "one"); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	m, err := search.UpsertMutation(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Bulk(ctx, def, []search.Mutation{m}); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	if _, err := d.Bulk(canceled, def, []search.Mutation{m}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := d.ready(canceled, def); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if query := compileFilter(search.And(search.Exists("code"))); query == nil {
		t.Fatal("missing conjunction query")
	}
	if err := d.ValidateIndex(ctx, def); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := d.ValidateIndex(ctx, search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
	if err := d.ready(ctx, search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
	f.fail = "search"
	if _, err := d.Search(ctx, def, search.Query{}); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := d.initialize(d.fingerprint); !errors.Is(err, search.ErrUnavailable) || d.index != nil {
		t.Fatal(err)
	}
	d.Close()
	if err := d.CreateIndex(ctx, def); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
}

// TestSearchResponseIntegrity rejects native results whose stored fields or facets cannot prove completeness.
func TestSearchResponseIntegrity(t *testing.T) {
	for _, scenario := range []string{"failed", "missing-body", "corrupt-body", "missing-facet", "corrupt-facet", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			d, def, f := bleveFixture(t)
			hit := &blevesearch.DocumentMatch{ID: "i6f6e65", Fields: map[string]any{"_forjoriginal": "one", "_forjbody": `{"active":true,"rank":1,"code":"a"}`}}
			terms := &blevesearch.TermFacets{}
			terms.Add(&blevesearch.TermFacet{Term: "v61", Count: 1}, &blevesearch.TermFacet{Term: "v62", Count: 1})
			f.result = &bleve.SearchResult{Status: &bleve.SearchStatus{}, Hits: blevesearch.DocumentMatchCollection{hit}, Total: 1, Facets: blevesearch.FacetResults{"categories": {Terms: terms}}}
			switch scenario {
			case "failed":
				f.result.Status.Failed = 1
			case "missing-body":
				delete(hit.Fields, "_forjbody")
			case "corrupt-body":
				hit.Fields["_forjbody"] = "[]"
			case "missing-facet":
				f.result.Facets = nil
			case "corrupt-facet":
				terms.Terms()[0].Term = "invalid"
			}
			result, err := d.Search(context.Background(), def, search.Query{Limit: 1, Facets: []search.TermFacet{{Name: "categories", Field: "category", Limit: 1}}})
			if scenario == "truncated" {
				if err != nil || !result.Facets[0].Truncated || len(result.Facets[0].Buckets) != 1 {
					t.Fatal(result, err)
				}
			} else if !errors.Is(err, search.ErrUnavailable) {
				t.Fatal(err)
			}
		})
	}
}

// TestFilesystemAcquisitionFailures checks path resolution, lock errors, and unreadable target metadata.
func TestFilesystemAcquisitionFailures(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := os.Symlink(locked+".owner.lock", locked+".owner.lock"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{Path: locked}); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	loop := filepath.Join(root, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, Config{Path: loop}); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	existing := filepath.Join(root, "existing")
	if err := os.WriteFile(existing, []byte("not an index"), 0600); err != nil {
		t.Fatal(err)
	}
	d := &Driver{cfg: Config{Path: existing}}
	if err := d.CreateIndex(ctx, searchtest.Definition()); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(root, "gone")
	if err := os.Mkdir(gone, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(gone); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Error(err)
		}
	}()
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(ctx, Config{Path: "relative"})
	if err != nil && !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if err == nil {
		defer opened.Close()
		if !filepath.IsAbs(opened.cfg.Path) {
			t.Fatal("relative ownership path retained")
		}
	}
}

// rejectedBatch models a native mapping implementation that rejects a projected document.
type rejectedBatch struct{}

// Index rejects mapping before any batch commit is attempted.
func (rejectedBatch) Index(string, any) error { return errors.New("private mapping failure") }

// Delete permits a valid preceding removal in an otherwise rejected batch.
func (rejectedBatch) Delete(string) {}

// TestBatchMappingFailureNeverCommits preserves native mapping error propagation across dependency updates.
func TestBatchMappingFailureNeverCommits(t *testing.T) {
	d, def, _ := bleveFixture(t)
	mutation, err := search.UpsertMutation(search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"a"}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.commitBatch(context.Background(), def, []search.Mutation{mutation}, rejectedBatch{}, func() error { t.Error("rejected batch was committed"); return nil })
	if !errors.Is(err, search.ErrInvalidDocument) {
		t.Fatal(err)
	}
}
