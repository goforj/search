package blevesearch_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/goforj/search"
	"github.com/goforj/search/driver/blevesearch"
	"github.com/goforj/search/searchtest"
)

// TestContract exercises Bleve through the same portable fixture used by service drivers.
func TestContract(t *testing.T) {
	for _, memory := range []bool{true, false} {
		name := "persistent"
		if memory {
			name = "memory"
		}
		t.Run(name, func(t *testing.T) {
			searchtest.RunBackend(t, func(ctx context.Context, def search.Definition) (*search.Index, func(context.Context) error, error) {
				cfg := blevesearch.Config{Memory: memory}
				if !memory {
					cfg.Path = filepath.Join(t.TempDir(), "index")
				}
				d, e := blevesearch.Open(ctx, cfg)
				if e != nil {
					return nil, nil, e
				}
				if e = d.CreateIndex(ctx, def); e != nil {
					_ = d.Close()
					return nil, nil, e
				}
				idx, e := search.NewIndex(d, def)
				return idx, func(context.Context) error {
					return d.Close()
				}, e
			})
		})
	}
}

// TestPersistenceAndDrift verifies ownership, reopen, and fail-closed provisioning.
func TestPersistenceAndDrift(t *testing.T) {
	ctx := context.Background()
	cfg := blevesearch.Config{Path: filepath.Join(t.TempDir(), "index")}
	d, e := blevesearch.Open(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if _, e = blevesearch.Open(ctx, cfg); !errors.Is(e, search.ErrConflict) {
		t.Fatal(e)
	}
	def := searchtest.Definition()
	if e = d.ValidateIndex(ctx, def); !errors.Is(e, search.ErrIndexNotFound) {
		t.Fatal(e)
	}
	if e = d.CreateIndex(ctx, def); e != nil {
		t.Fatal(e)
	}
	if e = d.CreateIndex(ctx, def); e != nil {
		t.Fatal(e)
	}
	changed := def
	changed.Fields = append(changed.Fields, search.Field{Name: "extra", Type: search.KeywordField})
	if e = d.CreateIndex(ctx, changed); !errors.Is(e, search.ErrSchemaDrift) {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	d, e = blevesearch.Open(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if e = d.ValidateIndex(ctx, def); e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	if e = d.ValidateIndex(ctx, def); !errors.Is(e, search.ErrClosed) {
		t.Fatal(e)
	}
}

// TestOpenAndProvisionFailures proves malformed configuration, failed acquisition, and inactive state stay explicit.
func TestOpenAndProvisionFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := blevesearch.Open(ctx, blevesearch.Config{Memory: true}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, config := range []blevesearch.Config{{}, {Memory: true, Path: "unexpected"}} {
		if _, err := blevesearch.Open(context.Background(), config); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not an index"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(file, "child"), file, file} {
		if _, err := blevesearch.Open(context.Background(), blevesearch.Config{Path: path}); !errors.Is(err, search.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	driver, err := blevesearch.Open(context.Background(), blevesearch.Config{Memory: true})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	definition := searchtest.Definition()
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(context.Background(), search.Query{}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	if _, err := index.Delete(context.Background(), "one"); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	if _, err := index.Upsert(context.Background(), search.Document{ID: "one", Body: []byte(`{"active":true,"rank":0,"code":"x"}`)}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	mutation, err := search.DeleteMutation("one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.Bulk(context.Background(), []search.Mutation{mutation}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	if _, err := driver.Wait(ctx, search.Receipt{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := driver.CreateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
	if err := driver.CreateIndex(context.Background(), definition); err != nil {
		t.Fatal(err)
	}
	changed := definition
	changed.Name = "changed"
	other, err := search.NewIndex(driver, changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Search(context.Background(), search.Query{}); !errors.Is(err, search.ErrSchemaDrift) {
		t.Fatal(err)
	}
}
