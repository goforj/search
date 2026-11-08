package sqlitesearch

import (
	"context"
	"errors"
	"github.com/goforj/search"
	"testing"
)

// TestInvalidNamespace fails configuration before attempting to use the application-owned pool.
func TestInvalidNamespace(t *testing.T) {
	if _, err := New(Config{Prefix: "invalid/name"}); !errors.Is(err, search.ErrInvalidDefinition) {
		t.Fatal(err)
	}
}

// TestLifecycleDelegation verifies every wrapper preserves the shared driver's closed-state behavior.
func TestLifecycleDelegation(t *testing.T) {
	d, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	def := search.Definition{}
	checks := []func() error{
		func() error { return d.CreateIndex(ctx, def) },
		func() error { return d.ValidateIndex(ctx, def) },
		func() error { _, err := d.Search(ctx, def, search.Query{}); return err },
		func() error { _, err := d.Upsert(ctx, def, search.Document{}); return err },
		func() error { _, err := d.Delete(ctx, def, "one"); return err },
		func() error { _, err := d.Bulk(ctx, def, nil); return err },
		func() error { _, err := d.Wait(ctx, search.Receipt{}); return err },
	}
	for _, call := range checks {
		if err := call(); !errors.Is(err, search.ErrClosed) {
			t.Fatal(err)
		}
	}
}
