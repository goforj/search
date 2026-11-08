package typesensesearch

import (
	"context"
	"errors"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// schema declares only explicitly owned fields and preserves the portable fingerprint.
func schema(name string, def search.Definition) map[string]any {
	fields := []map[string]any{
		{"name": "_forjid", "type": "string", "sort": true, "facet": false, "optional": false, "index": true},
		{"name": "_forjbody", "type": "string", "index": false, "sort": false, "facet": false, "optional": false},
		{"name": "_forjoriginal", "type": "string", "index": false, "sort": false, "facet": false, "optional": false},
	}
	for _, f := range def.Fields {
		kind := "string"
		if f.Type == search.IntegerField {
			kind = "int64"
		}
		if f.Type == search.KeywordsField {
			kind = "string[]"
		}
		fields = append(fields,
			map[string]any{
				"name": driverutil.FieldKey(f.Name), "type": kind,
				"optional": !f.Required, "facet": f.Filterable || f.Facetable,
				"sort": f.Sortable, "index": true,
			},
			map[string]any{
				"name": driverutil.FieldKey(f.Name) + "_exists", "type": "int32",
				"optional": false, "facet": true, "sort": false, "index": true,
			},
		)
	}
	fp, _ := def.Fingerprint()
	return map[string]any{
		"name": name, "fields": fields,
		"enable_nested_fields": false,
		"metadata":             map[string]any{"fingerprint": fp, "encoding": "goforj-search-v1"},
	}
}

// CreateIndex creates an absent collection and refuses to mutate an existing schema.
// @group Definitions
func (d *Driver) CreateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	i, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	def = i.Definition()
	name := d.target(def)
	var existing map[string]any
	err = d.http.Do(ctx, "GET", "/collections/"+name, nil, &existing, false)
	if err == nil {
		return d.validate(ctx, def)
	}
	if !errors.Is(err, search.ErrIndexNotFound) {
		return err
	}
	if err = d.http.Do(ctx, "POST", "/collections", schema(name, def), &existing, true); err != nil {
		return err
	}
	return d.validate(ctx, def)
}

// ValidateIndex compares actual field settings and persisted ownership metadata.
// @group Definitions
func (d *Driver) ValidateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return search.ErrClosed
	}
	i, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	return d.validate(ctx, i.Definition())
}

// validate compares field properties by name because the server may reorder schema fields.
func (d *Driver) validate(ctx context.Context, def search.Definition) error {
	name := d.target(def)
	var actual struct {
		Fields             []map[string]any
		Metadata           map[string]any
		EnableNestedFields bool `json:"enable_nested_fields"`
	}
	if err := d.http.Do(ctx, "GET", "/collections/"+name, nil, &actual, false); err != nil {
		return err
	}
	want := schema(name, def)
	fp, _ := def.Fingerprint()
	if actual.Metadata["fingerprint"] != fp || actual.Metadata["encoding"] != "goforj-search-v1" || actual.EnableNestedFields {
		return search.ErrSchemaDrift
	}
	wanted := want["fields"].([]map[string]any)
	if len(actual.Fields) != len(wanted) {
		return search.ErrSchemaDrift
	}
	byName := map[string]map[string]any{}
	for _, field := range actual.Fields {
		name, ok := field["name"].(string)
		if !ok {
			return search.ErrSchemaDrift
		}
		if _, exists := byName[name]; exists {
			return search.ErrSchemaDrift
		}
		byName[name] = field
	}
	for _, f := range wanted {
		for key, value := range f {
			if byName[f["name"].(string)][key] != value {
				return search.ErrSchemaDrift
			}
		}
	}
	d.name = name
	d.fingerprint = fp
	return nil
}
