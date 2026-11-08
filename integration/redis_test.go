//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"github.com/goforj/search/driver/redissearch"
	"github.com/goforj/search/searchtest"
	"io"
	"strings"
	"testing"
	"time"
)

// TestRedisContract proves the portable suite against authenticated Redis with its native Search module.
func TestRedisContract(t *testing.T) {
	endpoint := startTCPServer(t, "redis")
	searchtest.RunBackend(t, func(ctx context.Context, definition search.Definition) (*search.Index, func(context.Context) error, error) {
		driver, err := redissearch.New(redissearch.Config{Addr: endpoint, Password: testAPIKey, Prefix: fixturePrefix(t)})
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

// redisAdmin limits native administrative commands to this run's disposable Redis container.
func redisAdmin(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	fixturePool.Lock()
	container := fixturePool.engines["redis"]
	fixturePool.Unlock()
	command := []string{"redis-cli", "--no-auth-warning", "-a", testAPIKey, "--raw"}
	command = append(command, args...)
	code, output, err := container.Exec(ctx, command)
	if err != nil {
		t.Fatal("disposable Redis administration failed")
	}
	data, err := io.ReadAll(output)
	if err != nil || code != 0 {
		t.Fatal("disposable Redis command failed")
	}
	return string(data)
}

// TestRedisLifecycleAndBound verifies native authentication, schema alteration, and complete-match limits.
func TestRedisLifecycleAndBound(t *testing.T) {
	endpoint := startTCPServer(t, "redis")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info := redisAdmin(t, "INFO", "server")
	if !strings.Contains(info, "redis_version:8.10.3") {
		t.Fatal("wrong Redis server version")
	}
	modules := redisAdmin(t, "MODULE", "LIST")
	if !strings.Contains(modules, "search") {
		t.Fatal("native Search module is missing")
	}
	t.Log("verified Redis server and native Search module")
	wrong, err := redissearch.New(redissearch.Config{Addr: endpoint, Password: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	definition := searchtest.Definition()
	if err := wrong.ValidateIndex(ctx, definition); !errors.Is(err, search.ErrUnauthorized) {
		t.Fatal(err)
	}
	driver, err := redissearch.New(redissearch.Config{Addr: endpoint, Password: testAPIKey, Prefix: fixturePrefix(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.CreateIndex(ctx, definition); err != nil {
		t.Fatal(err)
	}
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	mutations := make([]search.Mutation, redissearch.MaxMatches+1)
	for n := range mutations {
		mutation, err := search.UpsertMutation(search.Document{ID: search.DocumentID(fmt.Sprintf("doc-%d", n)), Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
		if err != nil {
			t.Fatal(err)
		}
		mutations[n] = mutation
	}
	// Bypass the root batch budget to create the cardinality boundary with one atomic native submission.
	if _, err := driver.Bulk(ctx, index.Definition(), mutations); err != nil {
		t.Fatal(err)
	}
	if _, err := index.Search(ctx, search.Query{}); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	names := strings.Fields(redisAdmin(t, "FT._LIST"))
	var native string
	for _, name := range names {
		if strings.HasPrefix(name, fixturePrefix(t)) {
			native = name
		}
	}
	if native == "" {
		t.Fatal("owned native index not found")
	}
	redisAdmin(t, "FT.ALTER", native, "SCHEMA", "ADD", "unexpected", "TAG")
	if err := driver.ValidateIndex(ctx, definition); !errors.Is(err, search.ErrSchemaDrift) {
		t.Fatal(err)
	}
	redisAdmin(t, "FT.DROPINDEX", native)
	if _, err := index.Upsert(ctx, search.Document{ID: "after-drop", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	if _, err := index.Search(ctx, search.Query{}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
}
