package meilisearch

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
	"github.com/goforj/search/searchtest"
)

// fixtureDriver isolates protocol faults after explicit construction, without claiming real-driver conformance.
func fixtureDriver(t *testing.T, handler http.HandlerFunc) (*Driver, search.Definition) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	driver, err := New(Config{URL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { driver.Close() })
	index, err := search.NewIndex(driver, searchtest.Definition())
	if err != nil {
		t.Fatal(err)
	}
	definition := index.Definition()
	driver.name = driver.target(definition)
	driver.fingerprint, err = definition.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return driver, definition
}

// TestTaskInspectionFailures preserves terminal unknown, transport failure, and cancellation as different outcomes.
func TestTaskInspectionFailures(t *testing.T) {
	for _, status := range []string{"failed", "canceled", "invalid", "missing", "unauthorized"} {
		t.Run(status, func(t *testing.T) {
			driver, _ := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
				if status == "missing" {
					w.WriteHeader(404)
					return
				}
				if status == "unauthorized" {
					w.WriteHeader(401)
					return
				}
				fmt.Fprintf(w, `{"status":%q}`, status)
			})
			id := uint64(1)
			receipt, err := driver.remember(&id, nil)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := driver.Wait(context.Background(), receipt)
			switch status {
			case "failed", "canceled":
				if err != nil || outcome.State != search.OutcomeFailed || !errors.Is(outcome.Error, search.ErrMutationFailed) {
					t.Fatal(outcome, err)
				}
			case "missing":
				if err != nil || outcome.State != search.OutcomeUnknown || !errors.Is(outcome.Error, search.ErrReceiptExpired) {
					t.Fatal(outcome, err)
				}
			case "unauthorized":
				if !errors.Is(err, search.ErrUnauthorized) {
					t.Fatal(err)
				}
			default:
				if !errors.Is(err, search.ErrUnavailable) {
					t.Fatal(err)
				}
			}
		})
	}
	driver, _ := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"status":"processing"}`) })
	id := uint64(1)
	receipt, err := driver.remember(&id, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := driver.Wait(ctx, receipt); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if _, err := driver.remember(nil, nil); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	if _, err := driver.Wait(context.Background(), search.Receipt{}); err != nil {
		t.Fatal(err)
	}
	if err := driver.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Wait(context.Background(), receipt); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
}

// TestBulkInterruptedAcknowledgement retains accepted tasks after a later submission loses acknowledgement.
func TestBulkInterruptedAcknowledgement(t *testing.T) {
	driver, definition := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "delete-batch") {
			w.WriteHeader(503)
			fmt.Fprint(w, "private backend details")
			return
		}
		if r.Method == "GET" {
			fmt.Fprint(w, `{"status":"succeeded"}`)
			return
		}
		fmt.Fprint(w, `{"taskUid":7}`)
	})
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	upsert, err := search.UpsertMutation(search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := search.DeleteMutation("two")
	if err != nil {
		t.Fatal(err)
	}
	_, err = index.Bulk(context.Background(), []search.Mutation{upsert, deletion})
	var unknown *search.UnknownOutcomeError
	if !errors.As(err, &unknown) {
		t.Fatal(err)
	}
	receipt, ok := unknown.Receipt()
	if !ok {
		t.Fatal("accepted task lost")
	}
	outcome, err := index.Wait(context.Background(), receipt)
	if err != nil || outcome.State != search.OutcomeUnknown || outcome.Bulk == nil {
		t.Fatal(outcome, err)
	}
	for _, item := range outcome.Bulk.Items {
		if item.Status != search.ItemUnknown {
			t.Fatal("fabricated individual outcome")
		}
	}
}

// TestConstructionAndReadinessFailures keeps malformed transport policy and inactive clients explicit.
func TestConstructionAndReadinessFailures(t *testing.T) {
	for _, config := range []Config{{URL: "file:///tmp"}, {URL: "http://localhost", Prefix: "bad/prefix"}, {URL: "http://localhost", Prefix: strings.Repeat("a", 65)}} {
		if _, err := New(config); !errors.Is(err, search.ErrInvalidDefinition) {
			t.Fatal(err)
		}
	}
	driver, err := New(Config{URL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if _, err := driver.Search(context.Background(), searchtest.Definition(), search.Query{}); !errors.Is(err, search.ErrIndexNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := driver.CreateIndex(ctx, searchtest.Definition()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	driver.Close()
	if err := driver.CreateIndex(context.Background(), searchtest.Definition()); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
	if err := driver.ValidateIndex(context.Background(), searchtest.Definition()); !errors.Is(err, search.ErrClosed) {
		t.Fatal(err)
	}
}

// TestNativeTermBudget rejects potentially truncated queries before any server call.
func TestNativeTermBudget(t *testing.T) {
	for _, test := range []struct {
		terms []string
		valid bool
	}{
		{[]string{"café", "cappuccino"}, true},
		{strings.Fields("one two three four five six seven eight nine ten"), true},
		{strings.Fields("one two three four five six seven eight nine ten eleven"), false},
		{[]string{"東京京都大阪東京京都大"}, false},
		{[]string{"東京"}, true},
	} {
		if withinTermBudget(test.terms) != test.valid {
			t.Fatal(test)
		}
	}
}

// TestNativeCutoffIntegrity rejects incomplete or impossible processing metadata before exposing results.
func TestNativeCutoffIntegrity(t *testing.T) {
	for _, response := range []string{`{"hits":[],"estimatedTotalHits":0}`, `{"hits":[],"estimatedTotalHits":0,"processingTimeMs":60000}`} {
		driver, definition := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, response) })
		index, err := search.NewIndex(driver, definition)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := index.Search(context.Background(), search.Query{}); !errors.Is(err, search.ErrUnavailable) {
			t.Fatal(err)
		}
	}
}

// TestMissingLaterTaskRetainsRecovery preserves earlier accepted tasks after a later response omits its task ID.
func TestMissingLaterTaskRetainsRecovery(t *testing.T) {
	driver, definition := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"status":"succeeded"}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "delete-batch") {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprint(w, `{"taskUid":7}`)
	})
	index, err := search.NewIndex(driver, definition)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := search.UpsertMutation(search.Document{ID: "one", Body: []byte(`{"active":true,"rank":1,"code":"x"}`)})
	second, _ := search.DeleteMutation("two")
	_, err = index.Bulk(context.Background(), []search.Mutation{first, second})
	var unknown *search.UnknownOutcomeError
	if !errors.As(err, &unknown) {
		t.Fatal(err)
	}
	receipt, ok := unknown.Receipt()
	if !ok {
		t.Fatal("accepted task recovery was lost")
	}
	outcome, err := index.Wait(context.Background(), receipt)
	if err != nil || outcome.State != search.OutcomeUnknown || outcome.Bulk == nil || len(outcome.Bulk.Items) != 2 {
		t.Fatal(outcome, err)
	}
}

// TestExpiredTaskDominatesAggregateFailure keeps mixed task outcomes independent of task order.
func TestExpiredTaskDominatesAggregateFailure(t *testing.T) {
	for _, tasks := range [][]uint64{{7, 8}, {8, 7}} {
		driver, _ := fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/7") {
				w.WriteHeader(404)
				return
			}
			fmt.Fprint(w, `{"status":"failed"}`)
		})
		receipt := driver.rememberTasks(tasks, nil)
		outcome, err := driver.Wait(context.Background(), receipt)
		if err != nil || outcome.State != search.OutcomeUnknown || !errors.Is(outcome.Error, search.ErrReceiptExpired) {
			t.Fatal(outcome, err)
		}
	}
}
