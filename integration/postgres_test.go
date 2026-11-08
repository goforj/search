//go:build integration

package integration

import (
	"context"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/postgressearch"
	"github.com/goforj/search/searchtest"
	"testing"
)

// TestPostgresContract proves every portable fixture against the authenticated real PostgreSQL container.
func TestPostgresContract(t *testing.T) {
	db := sqlDatabase(t, "postgres")
	searchtest.RunBackend(t, func(ctx context.Context, definition search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := postgressearch.New(postgressearch.Config{DB: db, Prefix: fixturePrefix(t)})
		if err != nil {
			return nil, nil, err
		}
		if err := driver.CreateIndex(ctx, definition); err != nil {
			return nil, nil, err
		}
		if err := driver.CreateIndex(ctx, definition); err != nil {
			return nil, nil, err
		}
		if err := driver.ValidateIndex(ctx, definition); err != nil {
			return nil, nil, err
		}
		index, err := search.NewIndex(driver, definition)
		return index, func(context.Context) error { return driver.Close() }, err
	})
}
