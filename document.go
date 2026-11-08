package search

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Document retains explicit identity separately from application-owned JSON.
type Document struct {
	ID   DocumentID
	Body json.RawMessage
}

// Encode marshals a typed value into a validated JSON object.
// @group Documents
func Encode[T any](id DocumentID, value T) (Document, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return Document{}, ErrInvalidDocument
	}
	d := Document{id, b}
	_, _, err = normalizeDocument(d, Definition{}, DefaultLimits())
	return d, err
}

// Decode reads a document without requiring an ID field in the application type.
// @group Documents
func Decode[T any](d Document) (T, error) {
	var v T
	_, _, err := normalizeDocument(d, Definition{}, DefaultLimits())
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(d.Body, &v); err != nil {
		return v, ErrInvalidDocument
	}
	return v, nil
}

// Upsert encodes a typed value before replacing the complete document.
// @group Documents
func Upsert[T any](ctx context.Context, index *Index, id DocumentID, value T) (Receipt, error) {
	if err := ctx.Err(); err != nil {
		return Receipt{}, err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return Receipt{}, ErrInvalidDocument
	}
	return index.Upsert(ctx, Document{ID: id, Body: body})
}

// SearchAs decodes every hit and preserves its separate identity and metadata.
// @group Results
func SearchAs[T any](ctx context.Context, index *Index, q Query) (Page[T], error) {
	r, err := index.Search(ctx, q)
	if err != nil {
		return Page[T]{}, err
	}
	p := Page[T]{Total: r.Total, Facets: r.Facets, Took: r.Took}
	for _, h := range r.Hits {
		var v T
		e := json.Unmarshal(h.Document.Body, &v)
		if e != nil {
			return Page[T]{}, &DecodeError{ID: h.Document.ID}
		}
		p.Hits = append(p.Hits, HitOf[T]{h.Document.ID, v, h.Score, h.SortValues, h.Highlights})
	}
	return p, nil
}

// DecodeError identifies a malformed typed hit without disclosing its contents.
type DecodeError struct {
	ID DocumentID
}

// Error reports only the classification; callers may inspect ID under their disclosure policy.
// @group Results
func (e *DecodeError) Error() string {
	return "search: typed hit decode failed"
}

// Unwrap preserves invalid-document classification.
// @group Results
func (e *DecodeError) Unwrap() error {
	return ErrInvalidDocument
}

// validID accepts UTF-8 identities without allowing control bytes or unbounded metadata.
func validID(id DocumentID, l Limits) bool {
	if id == "" || len(id) > l.NameBytes || !utf8.ValidString(string(id)) {
		return false
	}
	for _, r := range id {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

// normalizeDocument uses a budget-aware parser before canonicalizing caller-owned JSON.
func normalizeDocument(d Document, def Definition, l Limits) (Document, map[string]any, error) {
	if !validID(d.ID, l) || len(d.Body) > l.DocumentBytes || !utf8.Valid(d.Body) || !validUnicodeEscapes(d.Body) {
		return Document{}, nil, ErrInvalidDocument
	}
	dec := json.NewDecoder(bytes.NewReader(d.Body))
	dec.UseNumber()
	members := 0
	v, err := readJSON(dec, 0, &members, l)
	if err != nil {
		return Document{}, nil, ErrInvalidDocument
	}
	if _, err = dec.Token(); err != io.EOF {
		return Document{}, nil, ErrInvalidDocument
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return Document{}, nil, ErrInvalidDocument
	}
	for _, f := range def.Fields {
		value, found := fieldValue(obj, f.Path)
		if !found || value == nil {
			if f.Required {
				return Document{}, nil, ErrInvalidDocument
			}
			continue
		}
		switch f.Type {
		case TextField, KeywordField:
			s, ok := value.(string)
			if !ok || len(s) > l.TextBytes {
				return Document{}, nil, ErrInvalidDocument
			}
		case BooleanField:
			if _, ok := value.(bool); !ok {
				return Document{}, nil, ErrInvalidDocument
			}
		case IntegerField:
			if _, ok := integerJSON(value); !ok {
				return Document{}, nil, ErrInvalidDocument
			}
		case KeywordsField:
			a, ok := value.([]any)
			if !ok {
				return Document{}, nil, ErrInvalidDocument
			}
			for _, item := range a {
				s, ok := item.(string)
				if !ok || len(s) > l.TextBytes {
					return Document{}, nil, ErrInvalidDocument
				}
			}
		}
	}
	b, err := json.Marshal(obj)
	if err != nil || len(b) > l.DocumentBytes {
		return Document{}, nil, ErrInvalidDocument
	}
	return Document{d.ID, b}, obj, nil
}

// readJSON bounds nesting and collection growth before allocating each child.
func readJSON(d *json.Decoder, depth int, members *int, l Limits) (any, error) {
	if depth > l.JSONDepth {
		return nil, ErrInvalidDocument
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch v := t.(type) {
	case json.Delim:
		switch v {
		case '{':
			m := map[string]any{}
			for d.More() {
				*members += 1
				if *members > l.ObjectMembers {
					return nil, ErrInvalidDocument
				}
				key, e := d.Token()
				if e != nil {
					return nil, e
				}
				k, ok := key.(string)
				if !ok || strings.HasPrefix(k, "_forj") {
					return nil, ErrInvalidDocument
				}
				if _, ok := m[k]; ok {
					return nil, ErrInvalidDocument
				}
				value, e := readJSON(d, depth+1, members, l)
				if e != nil {
					return nil, e
				}
				m[k] = value
			}
			_, err = d.Token()
			return m, err
		case '[':
			a := []any{}
			for d.More() {
				if len(a) >= l.ArrayLength {
					return nil, ErrInvalidDocument
				}
				value, e := readJSON(d, depth+1, members, l)
				if e != nil {
					return nil, e
				}
				a = append(a, value)
			}
			_, err = d.Token()
			return a, err
		default:
			return nil, ErrInvalidDocument
		}
	case string:
		if !utf8.ValidString(v) {
			return nil, ErrInvalidDocument
		}
		return v, nil
	case json.Number:
		if _, e := strconv.ParseFloat(string(v), 64); e != nil {
			return nil, ErrInvalidDocument
		}
		return v, nil
	case bool, nil:
		return v, nil
	default:
		return nil, ErrInvalidDocument
	}
}

// fieldValue follows declared JSON pointers rather than backend dotted-path conventions.
func fieldValue(obj map[string]any, p JSONPointer) (any, bool) {
	parts, ok := pointerParts(p, 256)
	if !ok {
		return nil, false
	}
	var v any = obj
	for _, part := range parts {
		switch n := v.(type) {
		case map[string]any:
			v, ok = n[part]
		case []any:
			i, e := strconv.Atoi(part)
			ok = e == nil && i >= 0 && i < len(n) && strconv.Itoa(i) == part
			if ok {
				v = n[i]
			}
		default:
			return nil, false
		}
		if !ok {
			return nil, false
		}
	}
	return v, true
}

// integerJSON accepts decimal integer spellings only within the common exact range.
func integerJSON(v any) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil && i >= -MaxInteger && i <= MaxInteger
}

// validUnicodeEscapes rejects unpaired surrogate escapes that encoding/json would otherwise replace silently.
func validUnicodeEscapes(body []byte) bool {
	inside := false
	for n := 0; n < len(body); n++ {
		if body[n] == '"' {
			inside = !inside
			continue
		}
		if !inside || body[n] != '\\' {
			continue
		}
		n++
		if n >= len(body) {
			return false
		}
		if body[n] != 'u' {
			continue
		}
		if n+4 >= len(body) {
			return false
		}
		value, err := strconv.ParseUint(string(body[n+1:n+5]), 16, 16)
		if err != nil {
			return false
		}
		n += 4
		if value >= 0xDC00 && value <= 0xDFFF {
			return false
		}
		if value < 0xD800 || value > 0xDBFF {
			continue
		}
		if n+6 >= len(body) || body[n+1] != '\\' || body[n+2] != 'u' {
			return false
		}
		low, err := strconv.ParseUint(string(body[n+3:n+7]), 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return false
		}
		n += 6
	}
	return true
}
