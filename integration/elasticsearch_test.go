//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/elasticsearch"
	"github.com/goforj/search/searchtest"
	"strings"
	"testing"
	"time"
)

// TestElasticsearchContract independently verifies Elasticsearch rather than assuming OpenSearch compatibility.
func TestElasticsearchContract(t *testing.T) {
	endpoint := startServer(t, "elasticsearch")
	searchtest.RunBackend(t, func(ctx context.Context, definition search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := elasticsearch.New(elasticsearch.Config{URL: endpoint, Username: "elastic", Password: testOpenSearchPassword, Prefix: fixturePrefix(t), RefreshInterval: 100 * time.Millisecond})
		if err != nil {
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

// TestElasticsearchMultiShardContract proves the portable contract across native shard routing and coordinator merges.
func TestElasticsearchMultiShardContract(t *testing.T) {
	endpoint := startServer(t, "elasticsearch")
	searchtest.RunBackend(t, func(ctx context.Context, definition search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := elasticsearch.New(elasticsearch.Config{URL: endpoint, Prefix: fixturePrefix(t), Username: "elastic", Password: testOpenSearchPassword, Shards: 2, RefreshInterval: 100 * time.Millisecond})
		if err != nil {
			return nil, nil, err
		}
		if err := driver.CreateIndex(ctx, definition); err != nil {
			driver.Close()
			return nil, nil, err
		}
		index, err := search.NewIndex(driver, definition)
		return index, func(context.Context) error { return driver.Close() }, err
	})
}

// TestElasticsearchPartialBulk proves a real mapping rejection remains visible beside a successful native item.
func TestElasticsearchPartialBulk(t *testing.T) {
	endpoint := startServer(t, "elasticsearch")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	driver, err := elasticsearch.New(elasticsearch.Config{URL: endpoint, Prefix: fixturePrefix(t), Username: "elastic", Password: testOpenSearchPassword, RefreshInterval: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	definition := searchtest.Definition()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	good, err := search.UpsertMutation(search.Document{ID: "good", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := search.UpsertMutation(search.Document{ID: "bad", Body: []byte(`{"active":true,"rank":"private-invalid-number","code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	// Intentionally bypass root validation so the real server must produce a per-item mapping failure.
	batch, err := driver.Bulk(ctx, index.Definition(), []search.Mutation{good, bad})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Items[0].Status != search.ItemAccepted || batch.Items[1].Status != search.ItemRejected || !errors.Is(batch.Items[1].Error, search.ErrInvalidDocument) {
		t.Fatal(batch.Items)
	}
	outcome, err := driver.Wait(ctx, batch.Receipt)
	if err != nil || outcome.State != search.OutcomeFailed || outcome.Bulk.Items[0].Status != search.ItemSucceeded || outcome.Bulk.Items[1].Status != search.ItemFailed {
		t.Fatal(outcome, err)
	}
	if strings.Contains(fmt.Sprintf("%+v", outcome.Bulk.Items), "private-invalid") {
		t.Fatal("native mapping reason leaked")
	}
}
