package typesensesearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/goforj/search"
	"net/http"
	"testing"
)

// TestProvisionProtocol verifies absent and existing collections and each administration request failure.
func TestProvisionProtocol(t *testing.T) {
	for _, existing := range []bool{false, true} {
		for failAt := 0; failAt <= 3; failAt++ {
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
					if r.Method == "POST" {
						exists = true
					}
					if !exists {
						w.WriteHeader(404)
						return
					}
					json.NewEncoder(w).Encode(schema(d.target(def), def))
				})
				err := d.CreateIndex(context.Background(), def)
				if failAt > 0 && failAt <= calls {
					if err == nil {
						t.Fatal("request failure ignored")
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

// TestMaterializedSchemaDrift rejects changed field lists, duplicate fields, and differing native field policy.
func TestMaterializedSchemaDrift(t *testing.T) {
	for _, scenario := range []string{"metadata", "nested", "field-count", "field-name", "duplicate-field", "field-type"} {
		t.Run(scenario, func(t *testing.T) {
			var d *Driver
			var def search.Definition
			d, def = fixtureDriver(t, func(w http.ResponseWriter, _ *http.Request) {
				actual := schema(d.target(def), def)
				fields := actual["fields"].([]map[string]any)
				switch scenario {
				case "metadata":
					actual["metadata"] = nil
				case "nested":
					actual["enable_nested_fields"] = true
				case "field-count":
					actual["fields"] = fields[:len(fields)-1]
				case "field-name":
					fields[0]["name"] = 3
				case "duplicate-field":
					fields[1]["name"] = fields[0]["name"]
				case "field-type":
					fields[0]["type"] = "int32"
				}
				json.NewEncoder(w).Encode(actual)
			})
			if err := d.ValidateIndex(context.Background(), def); !errors.Is(err, search.ErrSchemaDrift) {
				t.Fatal(err)
			}
		})
	}
}
