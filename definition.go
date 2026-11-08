package search

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FieldType defines the portable mapping shapes.
type FieldType string

const (
	// TextField enables analyzed full-text matching only.
	TextField FieldType = "text"
	// KeywordField enables exact scalar operations.
	KeywordField FieldType = "keyword"
	// BooleanField enables Boolean filtering, ordering, and facets.
	BooleanField FieldType = "boolean"
	// IntegerField enables exact integer filtering and ordering.
	IntegerField FieldType = "integer"
	// KeywordsField enables array membership and term facets.
	KeywordsField FieldType = "keywords"
)

// Direction selects portable scalar order.
type Direction string

const (
	// Ascending orders strings by UTF-8 bytes, Booleans false first, and integers numerically.
	Ascending Direction = "ascending"
	// Descending reverses the portable scalar order.
	Descending Direction = "descending"
)

// Sort selects a required, sortable field. Drivers append an ascending ID tie-break.
type Sort struct {
	Field     FieldName
	Direction Direction
}

// Field maps one declared name to an explicit document path.
type Field struct {
	Name       FieldName
	Path       JSONPointer
	Type       FieldType
	Required   bool
	Searchable bool
	Filterable bool
	Sortable   bool
	Facetable  bool
}

// Definition describes rebuildable index data and default query behavior.
type Definition struct {
	Name              IndexName
	Fields            []Field
	DefaultSearchable []FieldName
	DefaultSort       []Sort
}

// Validate checks a definition with conservative default limits.
// @group Definitions
func (d Definition) Validate() error {
	_, err := normalizeDefinition(d, DefaultLimits())
	return err
}

// Fingerprint returns a versioned canonical hash of the normalized search contract.
// @group Definitions
func (d Definition) Fingerprint() (string, error) {
	n, err := normalizeDefinition(d, DefaultLimits())
	if err != nil {
		return "", err
	}
	slices.SortFunc(n.Fields, func(a, b Field) int {
		return strings.Compare(string(a.Name), string(b.Name))
	})
	b, _ := json.Marshal(n)
	sum := sha256.Sum256(b)
	return "v1:" + hex.EncodeToString(sum[:]), nil
}

// cloneDefinition keeps mutable declarations out of the concurrent handle.
func cloneDefinition(d Definition) Definition {
	d.Fields = slices.Clone(d.Fields)
	d.DefaultSearchable = slices.Clone(d.DefaultSearchable)
	d.DefaultSort = slices.Clone(d.DefaultSort)
	return d
}

// validName keeps logical names safe across file and service encodings.
func validName(s string, max int) bool {
	if s == "" || len(s) > max || !utf8.ValidString(s) || strings.HasPrefix(s, "_forj") || strings.Contains(s, "..") {
		return false
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// normalizeDefinition resolves top-level paths and checks the portable type matrix.
func normalizeDefinition(d Definition, l Limits) (Definition, error) {
	if !validName(string(d.Name), l.NameBytes) || len(d.Fields) == 0 || len(d.Fields) > l.DefinitionFields || len(d.DefaultSort) > 2 || len(d.DefaultSearchable) > l.QueryFields {
		return Definition{}, ErrInvalidDefinition
	}
	remaining := l.DefinitionBytes
	for _, field := range d.Fields {
		if len(field.Path) > remaining {
			return Definition{}, ErrInvalidDefinition
		}
		remaining -= len(field.Path)
	}
	d = cloneDefinition(d)
	fields := map[FieldName]Field{}
	for x, f := range d.Fields {
		if !validName(string(f.Name), l.NameBytes) {
			return Definition{}, ErrInvalidDefinition
		}
		if _, ok := fields[f.Name]; ok {
			return Definition{}, ErrInvalidDefinition
		}
		if f.Path == "" {
			f.Path = JSONPointer("/" + strings.ReplaceAll(strings.ReplaceAll(string(f.Name), "~", "~0"), "/", "~1"))
		}
		if _, ok := pointerParts(f.Path, l.JSONPointerDepth); !ok {
			return Definition{}, ErrInvalidDefinition
		}
		switch f.Type {
		case TextField:
			if f.Filterable || f.Sortable || f.Facetable {
				return Definition{}, ErrInvalidDefinition
			}
		case KeywordField, BooleanField:
			if f.Searchable {
				return Definition{}, ErrInvalidDefinition
			}
		case IntegerField:
			if f.Searchable || f.Facetable {
				return Definition{}, ErrInvalidDefinition
			}
		case KeywordsField:
			if f.Searchable || f.Sortable {
				return Definition{}, ErrInvalidDefinition
			}
		default:
			return Definition{}, ErrInvalidDefinition
		}
		if f.Sortable && !f.Required {
			return Definition{}, ErrInvalidDefinition
		}
		d.Fields[x] = f
		fields[f.Name] = f
	}
	if len(d.DefaultSearchable) == 0 {
		for _, f := range d.Fields {
			if f.Searchable {
				d.DefaultSearchable = append(d.DefaultSearchable, f.Name)
			}
		}
	}
	seen := map[FieldName]bool{}
	for _, name := range d.DefaultSearchable {
		f, ok := fields[name]
		if !ok || !f.Searchable || seen[name] {
			return Definition{}, ErrInvalidDefinition
		}
		seen[name] = true
	}
	if !validSort(d.DefaultSort, fields) {
		return Definition{}, ErrInvalidDefinition
	}
	b, err := json.Marshal(d)
	if err != nil || len(b) > l.DefinitionBytes {
		return Definition{}, ErrInvalidDefinition
	}
	return d, nil
}

// validSort enforces required values and the two-field-plus-ID ordering budget.
func validSort(ss []Sort, fields map[FieldName]Field) bool {
	if len(ss) > 2 {
		return false
	}
	seen := map[FieldName]bool{}
	for _, s := range ss {
		f, ok := fields[s.Field]
		if !ok || !f.Sortable || seen[s.Field] || (s.Direction != Ascending && s.Direction != Descending) {
			return false
		}
		seen[s.Field] = true
	}
	return true
}

// pointerParts rejects malformed escape sequences and reserved metadata paths.
func pointerParts(p JSONPointer, max int) ([]string, bool) {
	s := string(p)
	if !utf8.ValidString(s) || !strings.HasPrefix(s, "/") {
		return nil, false
	}
	if strings.Count(s, "/") > max {
		return nil, false
	}
	parts := strings.Split(s[1:], "/")
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				j++
				if j == len(part) || (part[j] != '0' && part[j] != '1') {
					return nil, false
				}
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		if strings.HasPrefix(part, "_forj") {
			return nil, false
		}
		parts[i] = part
	}
	return parts, true
}
