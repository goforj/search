package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"net/http"
	"strings"
	"testing"
)

// TestProvisionProtocol validates creation, existing-index checks, and every remote administration failure.
func TestProvisionProtocol(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for failAt := 0; failAt <= 5; failAt++ {
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
					case r.Method == "PUT":
						exists = true
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if body["mappings"].(map[string]any)["dynamic"] != "strict" {
							t.Error("dynamic mapping enabled")
						}
						response = map[string]any{"acknowledged": true, "shards_acknowledged": true}
					case strings.HasSuffix(r.URL.Path, "/_mapping"):
						if !exists {
							w.WriteHeader(404)
							return
						}
						response = map[string]any{d.target(def): map[string]any{"mappings": indexMapping(def)}}
					case strings.HasSuffix(r.URL.Path, "/_settings"):
						response = map[string]any{d.target(def): map[string]any{"settings": map[string]any{"index": map[string]any{"number_of_shards": "1", "number_of_replicas": "0", "max_result_window": "16000", "refresh_interval": "100ms"}}}}
					default:
						response = map[string]any{"status": "green", "timed_out": false}
					}
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				})
				err := d.CreateIndex(context.Background(), def)
				if failAt > 0 && failAt <= calls {
					if err == nil {
						t.Fatal("administration failure was ignored")
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
				other := def
				other.Name = "different"
				if err := d.ready(context.Background(), other); !errors.Is(err, search.ErrSchemaDrift) {
					t.Fatal(err)
				}
			})
		}
	}
}

// TestProvisionResponseIntegrity distinguishes missing acknowledgements, materialized drift, and unhealthy clusters.
func TestProvisionResponseIntegrity(t *testing.T) {
	for _, scenario := range []string{"unacknowledged", "missing-map", "different-map", "missing-setting", "wrong-setting", "bad-setting-number", "missing-refresh", "bad-refresh", "unhealthy"} {
		t.Run(scenario, func(t *testing.T) {
			exists := false
			var d *Driver
			var def search.Definition
			d, def = fixtureDriver(t, func(w http.ResponseWriter, r *http.Request) {
				var response any
				switch {
				case r.Method == "PUT":
					exists = true
					response = map[string]any{"acknowledged": scenario != "unacknowledged", "shards_acknowledged": true}
				case strings.HasSuffix(r.URL.Path, "/_mapping"):
					if !exists {
						w.WriteHeader(404)
						return
					}
					mapping := indexMapping(def)
					if scenario == "different-map" {
						mapping["dynamic"] = "true"
					}
					response = map[string]any{d.target(def): map[string]any{"mappings": mapping}}
					if scenario == "missing-map" {
						response = map[string]any{}
					}
				case strings.HasSuffix(r.URL.Path, "/_settings"):
					settings := map[string]any{"number_of_shards": "1", "number_of_replicas": "0", "max_result_window": "16000", "refresh_interval": "100ms"}
					switch scenario {
					case "missing-setting":
						delete(settings, "number_of_shards")
					case "wrong-setting":
						settings["number_of_shards"] = "2"
					case "bad-setting-number":
						settings["number_of_shards"] = "bad"
					case "missing-refresh":
						delete(settings, "refresh_interval")
					case "bad-refresh":
						settings["refresh_interval"] = "bad"
					}
					response = map[string]any{d.target(def): map[string]any{"settings": map[string]any{"index": settings}}}
				default:
					response = map[string]any{"status": "red"}
				}
				json.NewEncoder(w).Encode(response)
			})
			err := d.CreateIndex(context.Background(), def)
			want := search.ErrSchemaDrift
			if scenario == "unacknowledged" || scenario == "unhealthy" {
				want = search.ErrUnavailable
			}
			if !errors.Is(err, want) {
				t.Fatal(err, want)
			}
		})
	}
}
