//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

// fixturePool owns each disposable engine for the entire package run, rather than an individual test.
var fixturePool = struct {
	sync.Mutex
	endpoints  map[string]string
	containers []testcontainers.Container
	engines    map[string]testcontainers.Container
}{endpoints: make(map[string]string), engines: make(map[string]testcontainers.Container)}

// TestMain tears down every started engine even when a test fails after partial startup.
func TestMain(m *testing.M) {
	code := m.Run()
	for n := len(fixturePool.containers) - 1; n >= 0; n-- {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := fixturePool.containers[n].Terminate(ctx)
		cancel()
		if err != nil {
			fmt.Fprintln(os.Stderr, "integration container cleanup failed")
			code = 1
		}
	}
	os.Exit(code)
}

// fixtureNamespaces assigns fresh physical namespaces even when go test repeats the same test name.
var fixtureNamespaces = struct {
	sync.Mutex
	next   uint64
	values map[*testing.T]string
}{values: make(map[*testing.T]string)}

// fixturePrefix keeps each test invocation isolated while remaining stable for all of its native operations.
func fixturePrefix(t *testing.T) string {
	t.Helper()
	fixtureNamespaces.Lock()
	defer fixtureNamespaces.Unlock()
	if prefix, ok := fixtureNamespaces.values[t]; ok {
		return prefix
	}
	fixtureNamespaces.next++
	prefix := fmt.Sprintf("t%012x_", fixtureNamespaces.next)
	fixtureNamespaces.values[t] = prefix
	t.Cleanup(func() { fixtureNamespaces.Lock(); delete(fixtureNamespaces.values, t); fixtureNamespaces.Unlock() })
	return prefix
}
