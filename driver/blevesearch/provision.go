package blevesearch

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// CreateIndex creates only an absent index; existing schema must match exactly.
// @group Definitions
func (d *Driver) CreateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check(ctx); err != nil {
		return err
	}
	i, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	def = i.Definition()
	if d.index != nil {
		return d.validate(def)
	}
	m := indexMapping(def)
	if d.cfg.Memory {
		d.index, err = bleve.NewMemOnly(m)
	} else {
		d.index, err = bleve.New(d.cfg.Path, m)
	}
	if err != nil {
		return search.ErrUnavailable
	}
	fp, _ := def.Fingerprint()
	if err = d.index.SetInternal([]byte("_forj_schema"), []byte(fp)); err != nil {
		_ = d.index.Close()
		d.index = nil
		return search.ErrUnavailable
	}
	d.fingerprint = fp
	return nil
}

// ValidateIndex compares both portable metadata and the actual materialized mapping.
// @group Definitions
func (d *Driver) ValidateIndex(ctx context.Context, def search.Definition) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.check(ctx); err != nil {
		return err
	}
	return d.validate(def)
}

// validate fails closed when either stored fingerprint or actual mapping has drifted.
func (d *Driver) validate(def search.Definition) error {
	if d.index == nil {
		return search.ErrIndexNotFound
	}
	i, err := search.NewIndex(d, def)
	if err != nil {
		return err
	}
	def = i.Definition()
	fp, _ := def.Fingerprint()
	stored, err := d.index.GetInternal([]byte("_forj_schema"))
	if err != nil {
		return search.ErrUnavailable
	}
	want, _ := json.Marshal(indexMapping(def))
	actual, _ := json.Marshal(d.index.Mapping())
	if string(stored) != fp || !bytes.Equal(want, actual) {
		return search.ErrSchemaDrift
	}
	d.fingerprint = fp
	return nil
}

// indexMapping explicitly disables inference, storing only the original body and identity.
func indexMapping(def search.Definition) *mapping.IndexMappingImpl {
	m := bleve.NewIndexMapping()
	m.IndexDynamic = false
	m.StoreDynamic = false
	m.DocValuesDynamic = false
	doc := bleve.NewDocumentMapping()
	doc.Dynamic = false
	for _, name := range []string{"_forjbody", "_forjoriginal"} {
		f := bleve.NewTextFieldMapping()
		f.Index = false
		f.Store = true
		f.IncludeInAll = false
		doc.AddFieldMappingsAt(name, f)
	}
	id := bleve.NewKeywordFieldMapping()
	id.Store = false
	id.IncludeInAll = false
	doc.AddFieldMappingsAt("_forjid", id)
	for _, field := range def.Fields {
		k := driverutil.FieldKey(field.Name)
		f := bleve.NewKeywordFieldMapping()
		if field.Type == search.TextField {
			f = bleve.NewTextFieldMapping()
			f.Analyzer = "standard"
		}
		f.Store = false
		f.IncludeInAll = false
		f.DocValues = true
		doc.AddFieldMappingsAt(k, f)
		exists := bleve.NewKeywordFieldMapping()
		exists.Store = false
		exists.IncludeInAll = false
		doc.AddFieldMappingsAt(k+"_exists", exists)
	}
	m.DefaultMapping = doc
	return m
}
