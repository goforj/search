package redissearch

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/goforj/search"
)

// TestRESPBounds rejects malformed framing and oversized or deeply nested allocation requests.
func TestRESPBounds(t *testing.T) {
	for _, frame := range []string{"", "+bad\n", "$-2\r\n", "$999999999\r\n", "*999999999\r\n", "$1\r\nx!!", ":not-a-number\r\n", "?unknown\r\n", strings.Repeat("*1\r\n", 18) + "+x\r\n", strings.Repeat("x", 65<<10) + "\r\n"} {
		reader := reader{input: bufio.NewReaderSize(strings.NewReader(frame), 64<<10), remaining: 1024}
		if _, err := reader.value(0); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
	for _, frame := range []string{"$-1\r\n", "*-1\r\n", "*2\r\n+text\r\n:7\r\n", "$3\r\nfoo\r\n"} {
		reader := reader{input: bufio.NewReader(strings.NewReader(frame)), remaining: 1024}
		if _, err := reader.value(0); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []any{nil, []any{"odd"}, []any{1, "value"}, []any{"same", 1, "same", 2}} {
		if _, err := pairs(value); !errors.Is(err, search.ErrUnavailable) {
			t.Fatal(err)
		}
	}
}

// TestErrorRedaction preserves stable native categories without returning server prose or uncertain write success.
func TestErrorRedaction(t *testing.T) {
	for message, want := range map[string]error{
		"WRONGPASS private-password": search.ErrUnauthorized, "NOPERM private-command": search.ErrUnauthorized,
		"Unknown Index name: private": search.ErrIndexNotFound, "SEARCH_INDEX_NOT_FOUND Index not found: private": search.ErrIndexNotFound,
		"INDEX_MISSING private": search.ErrIndexNotFound, "Index already exists private": search.ErrConflict,
		"unknown command FT.INFO private": search.ErrUnsupported, "query timeout private": search.ErrUnavailable,
	} {
		err := transportError(context.Background(), nativeError(message), false)
		if !errors.Is(err, want) || strings.Contains(err.Error(), "private") {
			t.Fatal(err)
		}
	}
	if !errors.Is(transportError(context.Background(), nativeError("private OOM"), true), search.ErrOutcomeUnknown) {
		t.Fatal("script failure lost uncertainty")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transportError(ctx, errors.New("private"), true); !errors.Is(err, context.Canceled) || !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
}

// TestConstructionRejectsInvalidPolicy rejects malformed connection and namespace settings without network I/O.
func TestConstructionRejectsInvalidPolicy(t *testing.T) {
	for _, config := range []Config{{Addr: "missing-port"}, {Addr: "localhost:6379", DB: -1}, {Addr: "localhost:6379", DB: 16}, {Addr: "localhost:6379", Timeout: time.Nanosecond}, {Addr: "localhost:6379", Timeout: 31 * time.Second}, {Addr: "localhost:6379", Prefix: "bad/prefix"}, {Addr: "localhost:6379", Prefix: strings.Repeat("a", 65)}, {Addr: "localhost:6379", Username: "user"}} {
		if _, err := New(config); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
	}
	tlsConfig := &tls.Config{ServerName: "original"}
	driver, err := New(Config{Addr: "localhost:6379", TLSConfig: tlsConfig})
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig.ServerName = "changed"
	if driver.config.TLSConfig.ServerName != "original" {
		t.Fatal("TLS policy was not cloned")
	}
	driver.Close()
	if err := driver.CreateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if err := driver.ValidateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := driver.Search(context.Background(), search.Definition{}, search.Query{}); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := driver.Wait(context.Background(), search.Receipt{}); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
}
