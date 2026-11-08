//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
}{endpoints: make(map[string]string)}

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

// fixturePrefix isolates native indexes while allowing all tests for an engine to reuse one container.
func fixturePrefix(t *testing.T) string {
	t.Helper()
	hash := sha256.Sum256([]byte(t.Name()))
	return "t" + hex.EncodeToString(hash[:6]) + "_"
}
