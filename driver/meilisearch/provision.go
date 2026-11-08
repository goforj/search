package meilisearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// nativeSearchCutoff exceeds every admitted HTTP deadline because Meilisearch 1.17 omits degradation from its wire response.
const nativeSearchCutoff = uint64(2 * driverutil.MaxRequestTimeout / time.Millisecond)

// settings includes every setting owned by this driver for drift validation.
func settings(def search.Definition) map[string]any {
	searchable, filterable, sortable := []string{}, []string{}, []string{"_forjid"}
	for _, f := range def.Fields {
		k := driverutil.FieldKey(f.Name)
		if f.Searchable {
			searchable = append(searchable, k)
		}
		if f.Filterable || f.Facetable {
			filterable = append(filterable, k)
		}
		filterable = append(filterable, k+"_exists")
		if f.Sortable {
			sortable = append(sortable, k)
		}
	}
	filterable = append(filterable, "_forjkind")
	slices.Sort(filterable)
	slices.Sort(sortable)
	return map[string]any{
		"searchableAttributes": searchable,
		"filterableAttributes": filterable,
		"sortableAttributes":   sortable,
		"displayedAttributes":  []string{"*"},
		"rankingRules":         []string{"sort", "words", "typo", "proximity", "attribute", "exactness", "_forjid:asc"},
		"pagination":           map[string]any{"maxTotalHits": 16000},
		"faceting": map[string]any{
			"maxValuesPerFacet": MaxFacetValues + 1,
			"sortFacetValuesBy": map[string]string{"*": "alpha"},
		},
		"typoTolerance":       map[string]any{"enabled": false},
		"stopWords":           []string{},
		"synonyms":            map[string]any{},
		"distinctAttribute":   nil,
		"searchCutoffMs":      nativeSearchCutoff,
		"prefixSearch":        "disabled",
		"separatorTokens":     []string{},
		"nonSeparatorTokens":  []string{},
		"dictionary":          []string{},
		"localizedAttributes": []string{},
	}

}

// CreateIndex provisions only missing state and never applies changes to an existing schema.
// @group Definitions
func (d *Driver) CreateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	idx, e := search.NewIndex(d, def)
	if e != nil {
		return e
	}
	def = idx.Definition()
	name := d.target(def)
	var info map[string]any
	e = d.http.Do(ctx, "GET", "/indexes/"+name, nil, &info, false)
	if e == nil {
		return d.validate(ctx, def)
	}
	if !errors.Is(e, search.ErrIndexNotFound) {
		return e
	}
	var task taskRef
	if e = d.http.Do(ctx, "POST", "/indexes", map[string]any{"uid": name, "primaryKey": "id"}, &task, true); e != nil {
		return e
	}
	if e = d.waitTask(ctx, task.TaskUID); e != nil {
		return e
	}
	if e = d.http.Do(ctx, "PATCH", "/indexes/"+name+"/settings", settings(def), &task, true); e != nil {
		return e
	}
	if e = d.waitTask(ctx, task.TaskUID); e != nil {
		return e
	}
	fp, _ := def.Fingerprint()
	meta := map[string]any{"id": "_forj_schema", "_forjkind": "schema", "fingerprint": fp}
	// Register searchable physical attributes even before the first application document exists.
	for _, field := range def.Fields {
		if field.Searchable {
			meta[driverutil.FieldKey(field.Name)] = ""
		}
	}
	if e = d.http.Do(ctx, "POST", "/indexes/"+name+"/documents", []any{meta}, &task, true); e != nil {
		return e
	}
	if e = d.waitTask(ctx, task.TaskUID); e != nil {
		return e
	}
	return d.validate(ctx, def)
}

// ValidateIndex compares portable identity and every driver-owned server setting.
// @group Definitions
func (d *Driver) ValidateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	i, e := search.NewIndex(d, def)
	if e != nil {
		return e
	}
	return d.validate(ctx, i.Definition())
}

// validate compares canonical settings with actual state rather than trusting stored metadata alone.
func (d *Driver) validate(ctx context.Context, def search.Definition) error {
	name := d.target(def)
	var info struct {
		PrimaryKey string `json:"primaryKey"`
	}
	if err := d.http.Do(ctx, "GET", "/indexes/"+name, nil, &info, false); err != nil {
		return err
	}
	if info.PrimaryKey != "id" {
		return search.ErrSchemaDrift
	}
	var actual map[string]json.RawMessage
	if e := d.http.Do(ctx, "GET", "/indexes/"+name+"/settings", nil, &actual, false); e != nil {
		return e
	}
	for k, want := range settings(def) {
		wb, _ := json.Marshal(want)
		var w, a any
		_ = json.Unmarshal(wb, &w)
		if e := json.Unmarshal(actual[k], &a); e != nil {
			return search.ErrSchemaDrift
		}
		if k == "typoTolerance" {
			am, ok := a.(map[string]any)
			if !ok || am["enabled"] != false {
				return search.ErrSchemaDrift
			}
			continue
		}
		ab, _ := json.Marshal(a)
		wb, _ = json.Marshal(w)
		if string(ab) != string(wb) {
			return search.ErrSchemaDrift
		}
	}
	var meta struct{ Fingerprint string }
	if e := d.http.Do(ctx, "GET", "/indexes/"+name+"/documents/_forj_schema", nil, &meta, false); e != nil {
		return search.ErrSchemaDrift
	}
	fp, _ := def.Fingerprint()
	if meta.Fingerprint != fp {
		return search.ErrSchemaDrift
	}
	d.name = name
	d.fingerprint = fp
	return nil
}

// taskRef represents a service-issued accepted operation identifier.
type taskRef struct {
	TaskUID *uint64 `json:"taskUid"`
}

// waitTask waits for asynchronous administration without retrying any mutation.
func (d *Driver) waitTask(ctx context.Context, id *uint64) error {
	if id == nil {
		return search.ErrOutcomeUnknown
	}
	for {
		var task struct{ Status string }
		if e := d.http.Do(ctx, "GET", fmt.Sprintf("/tasks/%d", *id), nil, &task, false); e != nil {
			return e
		}
		switch task.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			return search.ErrMutationFailed
		case "enqueued", "processing":
		default:
			return search.ErrUnavailable
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
