package meilisearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestProvisionProtocol checks asynchronous administration and every request failure in the sequence.
func TestProvisionProtocol(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for failAt := 0; failAt <= 11; failAt++ {
			t.Run(fmt.Sprintf("existing-%v/failure-%d", existing, failAt), func(t *testing.T) {
				failure := failAt
				calls := 0
				exists := existing
				var d *Driver
				var def search.Definition
				d, def = fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					if calls == failure {
						w.WriteHeader(503)
						return
					}
					var response any
					switch {
					case strings.HasPrefix(r.URL.Path, "/tasks/"):
						response = map[string]any{"status": "succeeded"}
					case r.Method != "GET":
						exists = true
						response = map[string]any{"taskUid": 1}
					case strings.HasSuffix(r.URL.Path, "/settings"):
						response = settings(def)
					case strings.HasSuffix(r.URL.Path, "/documents/_forj_schema"):
						response = map[string]any{"fingerprint": d.fingerprint}
					default:
						if !exists {
							w.WriteHeader(404)
							return
						}
						response = map[string]any{"primaryKey": "id"}
					}
					json.NewEncoder(w).Encode(response)
				})
				err := d.CreateIndex(context.Background(), def)
				if failAt > 0 && failAt <= calls {
					if err == nil {
						t.Fatal("request failure was ignored")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				failure = 0
				if err := d.ValidateIndex(context.Background(), def); err != nil {
					t.Fatal(err)
				}
				if err := d.CreateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
					t.Fatal(err)
				}
				if err := d.ValidateIndex(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
					t.Fatal(err)
				}
				if err := d.ready(context.Background(), search.Definition{}); !errors.Is(err, search.ErrInvalidDefinition) {
					t.Fatal(err)
				}
				changed := def
				changed.Name = "different"
				if err := d.ready(context.Background(), changed); !errors.Is(err, search.ErrSchemaDrift) {
					t.Fatal(err)
				}
			})
		}
	}
}

// TestMaterializedSettingsDrift rejects metadata-only agreement and wrong primary keys.
func TestMaterializedSettingsDrift(t *testing.T) {
	for _, scenario := range []string{"primary-key", "missing-setting", "typo-shape", "typo-enabled", "ranking", "fingerprint", "missing-meta"} {
		t.Run(scenario, func(t *testing.T) {
			var d *Driver
			var def search.Definition
			d, def = fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
				var response any
				switch {
				case strings.HasSuffix(r.URL.Path, "/settings"):
					actual := settings(def)
					switch scenario {
					case "missing-setting":
						delete(actual, "searchCutoffMs")
					case "typo-shape":
						actual["typoTolerance"] = false
					case "typo-enabled":
						actual["typoTolerance"] = map[string]any{"enabled": true}
					case "ranking":
						actual["rankingRules"] = []string{"words"}
					}
					response = actual
				case strings.HasSuffix(r.URL.Path, "/documents/_forj_schema"):
					if scenario == "missing-meta" {
						w.WriteHeader(404)
						return
					}
					fp := d.fingerprint
					if scenario == "fingerprint" {
						fp = "different"
					}
					response = map[string]any{"fingerprint": fp}
				default:
					key := "id"
					if scenario == "primary-key" {
						key = "different"
					}
					response = map[string]any{"primaryKey": key}
				}
				json.NewEncoder(w).Encode(response)
			})
			if err := d.ValidateIndex(context.Background(), def); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal(err)
			}
		})
	}
}

// TestAdministrativeTaskStates distinguishes terminal failure, malformed state, and interrupted polling.
func TestAdministrativeTaskStates(t *testing.T) {
	for _, status := range []string{"failed", "canceled", "invalid", "processing"} {
		t.Run(status, func(t *testing.T) {
			d, _ := fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode(map[string]any{"status": status})
			})
			id := uint64(1)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			err := d.waitTask(ctx, &id)
			want := search.ErrMutationFailed
			if status == "invalid" {
				want = search.ErrUnavailable
			}
			if status == "processing" {
				want = context.DeadlineExceeded
			}
			if !errors.Is(err, want) {
				t.Fatal(err)
			}
		})
	}
	d, _ := fixtureDriver(t, func(http.ResponseWriter, *http.Request) { t.Error("missing task reached transport") })
	if err := d.waitTask(context.Background(), nil); !errors.Is(err, search.ErrOutcomeUnknown) {
		t.Fatal(err)
	}
}
