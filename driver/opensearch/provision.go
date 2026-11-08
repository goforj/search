package opensearch

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// indexMapping disables inference and stores authoritative document bytes outside analyzed fields.
func indexMapping(definition search.Definition) map[string]any {
	properties := map[string]any{
		"_forjid":       map[string]any{"type": "keyword"},
		"_forjoriginal": map[string]any{"type": "keyword", "index": false, "doc_values": false},
		"_forjbody":     map[string]any{"type": "text", "index": false},
	}
	for _, field := range definition.Fields {
		key := driverutil.FieldKey(field.Name)
		mapping := map[string]any{"type": "keyword"}
		if field.Type == search.TextField {
			mapping = map[string]any{"type": "text", "analyzer": "standard"}
		}
		if field.Type == search.IntegerField {
			mapping = map[string]any{"type": "long"}
		}
		properties[key] = mapping
		properties[key+"_exists"] = map[string]any{"type": "keyword"}
	}
	fingerprint, _ := definition.Fingerprint()
	return map[string]any{
		"dynamic":    "strict",
		"properties": properties,
		"_meta":      map[string]any{"fingerprint": fingerprint, "encoding": "goforj-search-v1", "logical_index": string(definition.Name)},
	}
}

// settings enumerates index settings owned by this driver rather than relying on cluster defaults.
func (d *Driver) settings() map[string]any {
	return map[string]any{
		"number_of_shards":   d.config.Shards,
		"number_of_replicas": d.config.Replicas,
		"max_result_window":  16000,
		"refresh_interval":   d.config.RefreshInterval.String(),
	}
}

// CreateIndex creates only absent state and validates existing mappings without applying changes.
// @group Definitions
func (d *Driver) CreateIndex(ctx context.Context, definition search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	index, err := search.NewIndex(d, definition)
	if err != nil {
		return err
	}
	definition = index.Definition()
	name := d.target(definition)
	var existing map[string]any
	err = d.http.Do(ctx, "GET", "/"+name+"/_mapping", nil, &existing, false)
	if err == nil {
		return d.validate(ctx, definition)
	}
	if !errors.Is(err, search.ErrIndexNotFound) {
		return err
	}
	var result struct {
		Acknowledged       bool
		ShardsAcknowledged bool `json:"shards_acknowledged"`
	}
	body := map[string]any{"settings": d.settings(), "mappings": indexMapping(definition)}
	if err := d.http.Do(ctx, "PUT", "/"+name, body, &result, true); err != nil {
		return err
	}
	if !result.Acknowledged || !result.ShardsAcknowledged {
		return search.ErrUnavailable
	}
	return d.validate(ctx, definition)
}

// ValidateIndex compares actual mappings and every owned index setting before marking the handle ready.
// @group Definitions
func (d *Driver) ValidateIndex(ctx context.Context, definition search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	index, err := search.NewIndex(d, definition)
	if err != nil {
		return err
	}
	return d.validate(ctx, index.Definition())
}

// validate checks materialized state independently of the persisted portable fingerprint.
func (d *Driver) validate(ctx context.Context, definition search.Definition) error {
	name := d.target(definition)
	var mappings map[string]struct{ Mappings map[string]any }
	if err := d.http.Do(ctx, "GET", "/"+name+"/_mapping", nil, &mappings, false); err != nil {
		return err
	}
	actual, ok := mappings[name]
	if !ok {
		return search.ErrSchemaDrift
	}
	wanted, _ := json.Marshal(indexMapping(definition))
	observed, _ := json.Marshal(actual.Mappings)
	if string(wanted) != string(observed) {
		return search.ErrSchemaDrift
	}
	var settings map[string]struct {
		Settings struct{ Index map[string]json.RawMessage }
	}
	if err := d.http.Do(ctx, "GET", "/"+name+"/_settings?flat_settings=false", nil, &settings, false); err != nil {
		return err
	}
	owned := settings[name].Settings.Index
	for key, wanted := range map[string]int{"number_of_shards": d.config.Shards, "number_of_replicas": d.config.Replicas, "max_result_window": 16000} {
		var scalar string
		if json.Unmarshal(owned[key], &scalar) != nil {
			return search.ErrSchemaDrift
		}
		value, err := strconv.Atoi(scalar)
		if err != nil || value != wanted {
			return search.ErrSchemaDrift
		}
	}
	var refresh string
	if json.Unmarshal(owned["refresh_interval"], &refresh) != nil {
		return search.ErrSchemaDrift
	}
	interval, err := time.ParseDuration(refresh)
	if err != nil || interval != d.config.RefreshInterval {
		return search.ErrSchemaDrift
	}
	var health struct {
		Status   string
		TimedOut bool `json:"timed_out"`
	}
	if err := d.http.Do(ctx, "GET", "/_cluster/health/"+name+"?wait_for_status=green&timeout=1s", nil, &health, false); err != nil {
		return err
	}
	if health.TimedOut || health.Status != "green" {
		return search.ErrUnavailable
	}
	d.name = name
	d.fingerprint, err = definition.Fingerprint()
	return err
}
