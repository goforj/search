package postgressearch

import (
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
