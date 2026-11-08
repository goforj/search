package driverutil

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/goforj/search"
)

// TestHTTPStatusRedaction keeps server prose out of every public error chain.
func TestHTTPStatusRedaction(t *testing.T) {
	for status, expected := range map[int]error{
		401: search.ErrUnauthorized, 403: search.ErrUnauthorized, 404: search.ErrIndexNotFound,
		409: search.ErrConflict, 429: search.ErrRateLimited, 500: search.ErrUnavailable, 302: search.ErrUnavailable,
	} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, "secret-password private-document query-term")
			}))
			defer server.Close()
			client, err := NewHTTP(server.URL, "secret-password", "Authorization", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = client.Do(context.Background(), "GET", "/", nil, nil, false)
			if !errors.Is(err, expected) {
				t.Fatal(err)
			}
			for current := err; current != nil; current = errors.Unwrap(current) {
				if strings.Contains(fmt.Sprintf("%+v", current), "secret-password") {
					t.Fatal("secret leaked")
				}
			}
		})
	}
}

// TestHTTPRedirectIsolation prevents forwarding API credentials even to a same-origin redirect.
func TestHTTPRedirectIsolation(t *testing.T) {
	forwarded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			forwarded = true
		}
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := NewHTTP(server.URL, "secret", "Authorization", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Do(context.Background(), "GET", "/", nil, nil, false); err == nil {
		t.Fatal("redirect accepted")
	}
	if forwarded {
		t.Fatal("redirect forwarded")
	}
}

// TestHTTPBoundsAndMalformedMutation preserves uncertain acceptance for unreadable submission responses.
func TestHTTPBoundsAndMalformedMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "not-json-secret") }))
	defer server.Close()
	client, err := NewHTTP(server.URL, "", "Authorization", nil)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := client.Do(context.Background(), "POST", "/", map[string]string{"x": "one"}, &out, true); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	client.MaxBytes = 2
	if err := client.Do(context.Background(), "GET", "/", nil, &out, false); !errors.Is(err, search.ErrUnavailable) {
		t.Fatal(err)
	}
	if err := client.Do(context.Background(), "POST", "/", nil, &out, true); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Do(ctx, "GET", "/", nil, nil, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, base := range []string{"file:///tmp/private", "https://user:pass@example.com", "http://localhost?secret=yes", "http://localhost/#fragment", "http://localhost/base", "invalid"} {
		if _, err := NewHTTP(base, "", "", nil); err == nil {
			t.Fatal("invalid origin accepted")
		}
	}
}

// TestLedgerExpiration proves retention is bounded and returned outcomes are detached.
func TestLedgerExpiration(t *testing.T) {
	var ledger Ledger
	first, err := ledger.Complete(search.Outcome{State: search.OutcomeSucceeded, Bulk: &search.BulkOutcome{Items: []search.ItemResult{{Position: 0, ID: "one", Status: search.ItemSucceeded}}, Precision: search.PerItem}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := ledger.Wait(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	out.Bulk.Items[0].ID = "changed"
	out, err = ledger.Wait(context.Background(), first)
	if err != nil || out.Bulk.Items[0].ID != "one" {
		t.Fatal(out, err)
	}
	for n := 0; n < 4096; n++ {
		if _, err := ledger.Complete(search.Outcome{State: search.OutcomeSucceeded}); err != nil {
			t.Fatal(err)
		}
	}
	out, err = ledger.Wait(context.Background(), first)
	if err != nil || out.State != search.OutcomeUnknown || !errors.Is(out.Error, search.ErrReceiptExpired) {
		t.Fatal(out, err)
	}
}

// TestHTTPDeadlineCeiling preserves the ordering between client timeout and native service cutoff.
func TestHTTPDeadlineCeiling(t *testing.T) {
	for _, timeout := range []time.Duration{0, -1, time.Hour} {
		client, err := NewHTTP("http://localhost", "", "Authorization", &http.Client{Timeout: timeout})
		if err != nil || client.Client.Timeout != MaxRequestTimeout {
			t.Fatal(client, err)
		}
	}
	client, err := NewHTTP("http://localhost", "", "Authorization", &http.Client{Timeout: time.Second})
	if err != nil || client.Client.Timeout != time.Second {
		t.Fatal(client, err)
	}
}

// TestAcceptedMutationDeadlinePreservesUncertaintyAndContext refuses to describe a timeout as a rejected write.
func TestAcceptedMutationDeadlinePreservesUncertaintyAndContext(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	client, err := NewHTTP(server.URL, "", "Authorization", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err = client.Do(ctx, "POST", "/", map[string]string{"title": "private"}, nil, true)
	if !errors.Is(err, search.ErrOutcomeUnknown) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	safe := search.SafeError(err)
	if !errors.Is(safe, search.ErrOutcomeUnknown) || !errors.Is(safe, context.DeadlineExceeded) {
		t.Fatal(safe)
	}
}
