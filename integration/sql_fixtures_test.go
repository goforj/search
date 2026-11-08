//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/mattn/go-sqlite3"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sqlDatabase chooses a real native SQL engine without making the library own registration or pool policy.
func sqlDatabase(t *testing.T, engine string) *sql.DB {
	t.Helper()
	var db *sql.DB
	var err error
	if engine == "postgres" {
		endpoint := startTCPServer(t, engine)
		db, err = sql.Open("pgx", fmt.Sprintf("postgres://search:%s@%s/search?sslmode=disable", testAPIKey, endpoint))
	} else {
		db, err = sql.Open("sqlite3", filepath.Join(t.TempDir(), "search.db")+"?_busy_timeout=5000&_journal_mode=WAL")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	db.SetMaxOpenConns(4)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("native SQL engine is not ready")
	}
	var version string
	query := "SELECT sqlite_version()"
	if engine == "postgres" {
		query = "SHOW server_version"
	}
	if err := db.QueryRowContext(ctx, query).Scan(&version); err != nil {
		t.Fatal(err)
	}
	var versions map[string]engineVersion
	if err := json.Unmarshal(pinnedVersions, &versions); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(version, versions[engine].Version) {
		t.Fatalf("native %s version %q differs from fixture", engine, version)
	}
	t.Logf("native %s version %s", engine, version)
	return db
}
