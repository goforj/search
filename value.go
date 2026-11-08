package search

import (
	"encoding/json"
	"unicode/utf8"
)

// IndexName identifies an application-owned logical index.
type IndexName string

// FieldName identifies a declared mapping, independently of its JSON path.
type FieldName string

// DocumentID identifies a complete replacement document.
type DocumentID string

// JSONPointer addresses a value using RFC 6901.
type JSONPointer string

// ValueKind distinguishes portable scalar types.
type ValueKind string

const (
	// KeywordValue represents an exact UTF-8 string.
	KeywordValue ValueKind = "keyword"
	// BooleanValue represents a Boolean.
	BooleanValue ValueKind = "boolean"
	// IntegerValue represents an exactly portable signed integer.
	IntegerValue ValueKind = "integer"
	// MaxInteger is the largest exactly portable integer.
	MaxInteger int64 = 1<<53 - 1
)

// Value is an immutable typed filter, facet, or sort scalar. Its zero value is invalid.
type Value struct {
	kind   ValueKind
	text   string
	flag   bool
	number int64
}

// Keyword constructs a literal string without normalization.
// @group Values
func Keyword(v string) (Value, error) {
	if !utf8.ValidString(v) {
		return Value{}, ErrInvalidQuery
	}
	return Value{kind: KeywordValue, text: v}, nil
}

// Boolean constructs a Boolean scalar.
// @group Values
func Boolean(v bool) Value {
	return Value{kind: BooleanValue, flag: v}
}

// Integer rejects values outside the exact common integer range.
// @group Values
func Integer(v int64) (Value, error) {
	if v < -MaxInteger || v > MaxInteger {
		return Value{}, ErrInvalidQuery
	}
	return Value{kind: IntegerValue, number: v}, nil
}

// Kind reports the scalar's discriminator.
// @group Values
func (v Value) Kind() ValueKind {
	return v.kind
}

// Keyword returns the string only for keyword values.
// @group Values
func (v Value) Keyword() (string, bool) {
	return v.text, v.kind == KeywordValue
}

// Boolean returns the Boolean only for Boolean values.
// @group Values
func (v Value) Boolean() (bool, bool) {
	return v.flag, v.kind == BooleanValue
}

// Integer returns the integer only for integer values.
// @group Values
func (v Value) Integer() (int64, bool) {
	return v.number, v.kind == IntegerValue
}

// MarshalJSON preserves a Value's scalar JSON shape for typed result DTOs and rejects its invalid zero value.
// @group Values
func (v Value) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case KeywordValue:
		return json.Marshal(v.text)
	case BooleanValue:
		return json.Marshal(v.flag)
	case IntegerValue:
		return json.Marshal(v.number)
	default:
		return nil, ErrInvalidQuery
	}
}
