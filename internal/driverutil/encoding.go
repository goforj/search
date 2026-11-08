// Package driverutil implements physical encoding shared by first-party drivers.
package driverutil

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/goforj/search"
)

// FieldKey keeps logical names away from backend query syntaxes.
func FieldKey(n search.FieldName) string {
	return "f" + hex.EncodeToString([]byte(n))
}

// IDKey preserves the UTF-8 byte order of original IDs.
func IDKey(id search.DocumentID) string {
	return "i" + hex.EncodeToString([]byte(id))
}

// Scalar creates a nonempty, order-preserving exact-match representation.
func Scalar(v search.Value) string {
	switch v.Kind() {
	case search.KeywordValue:
		s, _ := v.Keyword()
		return "v" + hex.EncodeToString([]byte(s))
	case search.BooleanValue:
		b, _ := v.Boolean()
		if b {
			return "1"
		}
		return "0"
	case search.IntegerValue:
		n, _ := v.Integer()
		return fmt.Sprintf("%016x", uint64(n)^uint64(1<<63))
	}
	return ""
}

// NativeValue converts validated JSON fields to exact typed scalar values.
func NativeValue(v any, t search.FieldType) search.Value {
	switch t {
	case search.KeywordField, search.KeywordsField:
		s, _ := v.(string)
		x, _ := search.Keyword(s)
		return x
	case search.BooleanField:
		b, _ := v.(bool)
		return search.Boolean(b)
	case search.IntegerField:
		n, _ := v.(json.Number)
		i, _ := n.Int64()
		x, _ := search.Integer(i)
		return x
	}
	return search.Value{}
}

// Object decodes canonical JSON while preserving portable integer precision.
func Object(d search.Document) map[string]any {
	dec := json.NewDecoder(bytes.NewReader(d.Body))
	dec.UseNumber()
	var m map[string]any
	_ = dec.Decode(&m)
	return m
}

// At follows a normalized RFC 6901 pointer through objects and arrays.
func At(m map[string]any, p search.JSONPointer) (any, bool) {
	var v any = m
	for _, part := range strings.Split(string(p)[1:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch x := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = x[part]
			if !ok {
				return nil, false
			}
		case []any:
			i, e := strconv.Atoi(part)
			if e != nil || i < 0 || i >= len(x) || strconv.Itoa(i) != part {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, v != nil
}

// Physical projects declared fields without exposing backend metadata in returned documents.
func Physical(d search.Document, def search.Definition) map[string]any {
	obj := Object(d)
	m := map[string]any{"_forjid": IDKey(d.ID), "_forjbody": string(d.Body), "_forjoriginal": string(d.ID)}
	for _, f := range def.Fields {
		v, ok := At(obj, f.Path)
		if !ok {
			continue
		}
		k := FieldKey(f.Name)
		m[k+"_exists"] = "1"
		switch f.Type {
		case search.TextField:
			m[k] = v
		case search.KeywordsField:
			a := v.([]any)
			ss := make([]string, 0, len(a))
			for _, item := range a {
				ss = append(ss, Scalar(NativeValue(item, f.Type)))
			}
			m[k] = ss
		default:
			m[k] = Scalar(NativeValue(v, f.Type))
		}
	}
	return m
}

// Terms strips query operators so every driver receives literal word tokens.
func Terms(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// Highlights returns structured full-field fragments without parsing or trusting backend HTML.
func Highlights(d search.Document, def search.Definition, q search.Query) map[search.FieldName][]search.Fragment {
	return highlights(Object(d), def, q)
}

// highlights reuses the validated stored object so response decoding does not parse the body for each feature.
func highlights(obj map[string]any, def search.Definition, q search.Query) map[search.FieldName][]search.Fragment {
	out := map[search.FieldName][]search.Fragment{}
	terms := map[string]bool{}
	for _, t := range Terms(q.Text) {
		terms[t] = true
	}
	for _, h := range q.Highlight {
		for _, f := range def.Fields {
			if f.Name != h.Field {
				continue
			}
			v, ok := At(obj, f.Path)
			if !ok {
				continue
			}
			text := v.(string)
			segments := []search.Segment{}
			start := 0
			word := -1
			flush := func(end int) {
				if word < 0 {
					return
				}
				if word > start {
					segments = append(segments, search.Segment{Text: text[start:word]})
				}
				segments = append(segments, search.Segment{Text: text[word:end], Matched: terms[strings.ToLower(text[word:end])]})
				start = end
				word = -1
			}
			for pos, r := range text {
				if unicode.IsLetter(r) || unicode.IsDigit(r) {
					if word < 0 {
						word = pos
					}
				} else {
					flush(pos)
				}
			}
			flush(len(text))
			if start < len(text) {
				segments = append(segments, search.Segment{Text: text[start:]})
			}
			out[h.Field] = []search.Fragment{{Segments: segments}}
		}
	}
	return out
}

// Hit restores stored application data and public sort values from a physical hit.
func Hit(id, body string, def search.Definition, q search.Query, score *float64) (search.Hit, error) {
	d := search.Document{ID: search.DocumentID(id), Body: json.RawMessage(body)}
	if !json.Valid(d.Body) {
		return search.Hit{}, search.ErrUnavailable
	}
	obj := Object(d)
	if !validStoredFields(obj, def) {
		return search.Hit{}, search.ErrUnavailable
	}
	h := search.Hit{Document: d, Score: score, Highlights: highlights(obj, def, q)}
	for _, s := range q.Sort {
		for _, f := range def.Fields {
			if f.Name == s.Field {
				v, ok := At(obj, f.Path)
				if !ok {
					return search.Hit{}, search.ErrUnavailable
				}
				h.SortValues = append(h.SortValues, NativeValue(v, f.Type))
			}
		}
	}
	return h, nil
}

// DecodeScalar restores exact encoded facet values for their declared field type.
func DecodeScalar(s string, t search.FieldType) (search.Value, error) {
	switch t {
	case search.KeywordField, search.KeywordsField:
		if !strings.HasPrefix(s, "v") {
			return search.Value{}, search.ErrUnavailable
		}
		b, e := hex.DecodeString(s[1:])
		if e != nil {
			return search.Value{}, search.ErrUnavailable
		}
		value, err := search.Keyword(string(b))
		if err != nil {
			return search.Value{}, search.ErrUnavailable
		}
		return value, nil
	case search.BooleanField:
		if s != "0" && s != "1" {
			return search.Value{}, search.ErrUnavailable
		}
		return search.Boolean(s == "1"), nil
	}
	return search.Value{}, search.ErrUnavailable
}

// validStoredFields checks persisted data before highlight and facet code uses typed projections.
func validStoredFields(object map[string]any, definition search.Definition) bool {
	if object == nil {
		return false
	}
	for _, field := range definition.Fields {
		value, present := At(object, field.Path)
		if !present {
			if field.Required {
				return false
			}
			continue
		}
		switch field.Type {
		case search.TextField, search.KeywordField:
			if _, ok := value.(string); !ok {
				return false
			}
		case search.BooleanField:
			if _, ok := value.(bool); !ok {
				return false
			}
		case search.IntegerField:
			number, ok := value.(json.Number)
			if !ok {
				return false
			}
			integer, err := number.Int64()
			if err != nil || integer < -search.MaxInteger || integer > search.MaxInteger {
				return false
			}
		case search.KeywordsField:
			values, ok := value.([]any)
			if !ok {
				return false
			}
			for _, value := range values {
				if _, ok := value.(string); !ok {
					return false
				}
			}
		default:
			return false
		}
	}
	return true
}
