package redissearch

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// schema captures only owned native schema state, excluding volatile counters and memory statistics.
func schema(value any) (string, error) {
	info, err := pairs(value)
	if err != nil {
		return "", err
	}
	state := map[string]any{}
	for _, key := range []string{"index_definition", "attributes", "index_options", "stopwords_list"} {
		value, ok := info[key]
		if !ok && key != "stopwords_list" {
			return "", search.ErrUnavailable
		}
		state[key] = value
	}
	data, err := json.Marshal(state)
	if err != nil {
		return "", search.ErrUnavailable
	}
	return string(data), nil
}

// CreateIndex provisions absent native schema without stemming or stop-word dropping and waits for initial indexing.
// @group Definitions
func (d *Driver) CreateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	index, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	def = index.Definition()
	name := d.target(def)
	_, err = d.command(ctx, false, "FT.INFO", name)
	if err == nil {
		return d.validate(ctx, def)
	}
	if !errors.Is(err, search.ErrIndexNotFound) {
		return err
	}
	args := []string{"FT.CREATE", name, "ON", "HASH", "PREFIX", "1", name + ":doc:", "STOPWORDS", "0", "SCHEMA", "_forjid", "TAG", "CASESENSITIVE"}
	for _, field := range def.Fields {
		key := driverutil.FieldKey(field.Name)
		args = append(args, key)
		switch field.Type {
		case search.TextField:
			args = append(args, "TEXT", "NOSTEM")
		case search.IntegerField:
			args = append(args, "NUMERIC")
		default:
			args = append(args, "TAG", "CASESENSITIVE", "SEPARATOR", ",")
		}
		args = append(args, key+"_exists", "TAG", "CASESENSITIVE")
	}
	if _, err := d.command(ctx, true, args...); err != nil {
		return err
	}
	info, err := d.indexInfo(ctx, name)
	if err != nil {
		return err
	}
	layout, err := schema(info)
	if err != nil {
		return err
	}
	fingerprint, _ := def.Fingerprint()
	metadata, _ := json.Marshal(map[string]string{"fingerprint": fingerprint, "layout": layout})
	if _, err := d.command(ctx, true, "SET", name+":meta", string(metadata), "NX"); err != nil {
		return err
	}
	return d.validate(ctx, def)
}

// ValidateIndex waits for initial indexing and checks portable ownership and actual native attributes without changing either.
// @group Definitions
func (d *Driver) ValidateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	index, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	return d.validate(ctx, index.Definition())
}

// validate detects native alteration even when the stored fingerprint remains unchanged.
func (d *Driver) validate(ctx context.Context, def search.Definition) error {
	name := d.target(def)
	info, err := d.indexInfo(ctx, name)
	if err != nil {
		return err
	}
	layout, err := schema(info)
	if err != nil {
		return err
	}
	value, err := d.command(ctx, false, "GET", name+":meta")
	if err != nil {
		return err
	}
	metadata, ok := value.(string)
	if !ok {
		return search.ErrSchemaDrift
	}
	var expected struct{ Fingerprint, Layout string }
	if json.Unmarshal([]byte(metadata), &expected) != nil {
		return search.ErrSchemaDrift
	}
	fingerprint, _ := def.Fingerprint()
	if expected.Fingerprint != fingerprint || expected.Layout != layout {
		return search.ErrSchemaDrift
	}
	d.name, d.fingerprint = name, fingerprint
	return nil
}

// indexInfo waits for the initial keyspace scan so provisioning never advertises partial search visibility.
func (d *Driver) indexInfo(ctx context.Context, name string) (any, error) {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d.config.Timeout)
		defer cancel()
	}
	for {
		value, err := d.command(ctx, false, "FT.INFO", name)
		if err != nil {
			return nil, err
		}
		info, err := pairs(value)
		if err != nil {
			return nil, err
		}
		indexing, ok := info["indexing"].(int64)
		if !ok || indexing != 0 && indexing != 1 {
			return nil, search.ErrUnavailable
		}
		if indexing == 0 {
			return value, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
