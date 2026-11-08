package blevesearch_test

import (
	"context"
	"errors"
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
