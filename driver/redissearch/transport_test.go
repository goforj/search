package redissearch

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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

// protocolServer makes connection loss and cancellation reproducible below the real-engine integration layer.
func protocolServer(t *testing.T, handler func(net.Conn)) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(3 * time.Second))
		handler(connection)
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Error("protocol fixture did not stop")
		}
	})
	return listener.Addr().String()
}

// readCommand keeps fake server assertions at the protocol boundary without recording credential values.
func readCommand(t *testing.T, connection net.Conn, want string) {
	t.Helper()
	reader := reader{input: bufio.NewReader(connection), remaining: maxResponseBytes}
	value, err := reader.value(0)
	if err != nil {
		t.Error("client did not send a complete command")
		return
	}
	args, ok := value.([]any)
	if !ok || len(args) == 0 || args[0] != want {
		t.Error("client sent an unexpected command kind")
	}
}

// TestConnectionStages verifies authentication, database selection, and uncertain dispatch independently.
func TestConnectionStages(t *testing.T) {
	t.Run("authenticated-database", func(t *testing.T) {
		addr := protocolServer(t, func(connection net.Conn) {
			for _, command := range []string{"AUTH", "SELECT", "PING"} {
				readCommand(t, connection, command)
				fmt.Fprint(connection, "+OK\r\n")
			}
		})
		driver, err := New(Config{Addr: addr, Username: "fixture-user", Password: "fixture-password", DB: 2})
		if err != nil {
			t.Fatal(err)
		}
		defer driver.Close()
		if value, err := driver.command(context.Background(), false, "PING"); err != nil || value != "OK" {
			t.Fatal(value, err)
		}
	})
	for _, stage := range []string{"AUTH", "SELECT"} {
		t.Run("rejected-"+stage, func(t *testing.T) {
			addr := protocolServer(t, func(connection net.Conn) {
				readCommand(t, connection, stage)
				fmt.Fprint(connection, "-NOPERM private-credential-and-command\r\n")
			})
			config := Config{Addr: addr, DB: 1}
			if stage == "AUTH" {
				config.Password = "fixture-password"
			}
			driver, err := New(config)
			if err != nil {
				t.Fatal(err)
			}
			defer driver.Close()
			if _, err := driver.command(context.Background(), true, "EVAL", "fixture"); !errors.Is(err, search.ErrUnauthorized) || errors.Is(err, search.ErrOutcomeUnknown) || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
		})
	}
	t.Run("lost-write-response", func(t *testing.T) {
		addr := protocolServer(t, func(connection net.Conn) { readCommand(t, connection, "EVAL") })
		driver, err := New(Config{Addr: addr})
		if err != nil {
			t.Fatal(err)
		}
		defer driver.Close()
		if _, err := driver.command(context.Background(), true, "EVAL", "fixture"); !errors.Is(err, search.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	})
	t.Run("canceled-write", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		addr := protocolServer(t, func(connection net.Conn) {
			readCommand(t, connection, "EVAL")
			cancel()
			var b [1]byte
			connection.Read(b[:])
		})
		driver, err := New(Config{Addr: addr})
		if err != nil {
			t.Fatal(err)
		}
		defer driver.Close()
		if _, err := driver.command(ctx, true, "EVAL", "fixture"); !errors.Is(err, context.Canceled) || !errors.Is(err, search.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	})
	t.Run("malformed-read-response", func(t *testing.T) {
		addr := protocolServer(t, func(connection net.Conn) {
			readCommand(t, connection, "PING")
			fmt.Fprint(connection, "$999999999\r\n")
		})
		driver, err := New(Config{Addr: addr})
		if err != nil {
			t.Fatal(err)
		}
		defer driver.Close()
		if _, err := driver.command(context.Background(), false, "PING"); !errors.Is(err, search.ErrUnavailable) {
			t.Fatal(err)
		}
	})
}

// TestTLSVerification refuses an untrusted server before dispatching any Redis mutation.
func TestTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("untrusted TLS server received an application request")
	}))
	defer server.Close()
	driver, err := New(Config{Addr: strings.TrimPrefix(server.URL, "https://"), TLSConfig: &tls.Config{}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if _, err := driver.command(context.Background(), true, "EVAL", "fixture"); !errors.Is(err, search.ErrUnavailable) || errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
}

// deadlineFailureConnection fails before credentials or commands can be transmitted.
type deadlineFailureConnection struct{ net.Conn }

// SetDeadline reproduces a connection closed between dialing and deadline installation.
func (deadlineFailureConnection) SetDeadline(time.Time) error {
	return errors.New("private deadline failure")
}

// TestTransportEdges verifies bounded encoding and native errors without relying on network races.
func TestTransportEdges(t *testing.T) {
	for _, tc := range []struct {
		frame     string
		remaining int
	}{{"$3\r\nfoo\r\n", 8}, {"$3\r\nf", 1024}, {"*1000001\r\n", maxResponseBytes}} {
		r := reader{input: bufio.NewReader(strings.NewReader(tc.frame)), remaining: tc.remaining}
		if _, err := r.value(0); err == nil {
			t.Fatal("malformed or excessive response accepted")
		}
	}
	first, second := net.Pipe()
	defer first.Close()
	defer second.Close()
	d, err := New(Config{Addr: "localhost:6379"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.connected(context.Background(), deadlineFailureConnection{Conn: first}, false, []string{"PING"}); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := exchange(second, []string{strings.Repeat("a", maxResponseBytes+1)}); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := exchange(second, []string{"PING"}); err == nil {
		t.Fatal("closed pipe accepted a write")
	}
	if err := transportError(context.Background(), search.ErrUnsupported, false); !errors.Is(err, search.ErrUnsupported) {
		t.Fatal(err)
	}
	for _, native := range []nativeError{"NOAUTH private", "query timeout private"} {
		err := transportError(context.Background(), native, true)
		if !errors.Is(err, search.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := transportError(ctx, errors.New("private"), false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// TestRESPAggregateAllocationBudget rejects array reservations before allocating their element storage.
func TestRESPAggregateAllocationBudget(t *testing.T) {
	for _, frame := range []string{fmt.Sprintf("*%d\r\n", 1024/(2*(strconv.IntSize/8))), fmt.Sprintf("*2\r\n*%d\r\n", 1024/(2*(strconv.IntSize/8))-2)} {
		r := reader{input: bufio.NewReader(strings.NewReader(frame)), remaining: 1024}
		if _, err := r.value(0); !errors.Is(err, search.ErrUnavailable) {
			t.Fatalf("array allocation exceeded response budget: %v", err)
		}
	}
	frame := "*2\r\n$-1\r\n$-1\r\n"
	r := reader{input: bufio.NewReader(strings.NewReader(frame)), remaining: len(frame) + 2*2*(strconv.IntSize/8)}
	if _, err := r.value(0); err != nil || r.remaining != 0 {
		t.Fatalf("exact allocation budget rejected: remaining=%d, error=%v", r.remaining, err)
	}
}
