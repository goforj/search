package search

import "time"

// TotalRelation describes the accuracy of a result count.
type TotalRelation string

const (
	// TotalExact reports a complete count.
	TotalExact TotalRelation = "exact"
	// TotalEstimated reports an approximate count.
	TotalEstimated TotalRelation = "estimated"
	// TotalLowerBound reports a proven minimum.
	TotalLowerBound TotalRelation = "lower_bound"
)

// Total retains count precision independently of pagination.
type Total struct {
	Value    uint64
	Relation TotalRelation
}

// CountPrecision describes term-bucket count accuracy.
type CountPrecision string

const (
	// CountExact reports complete term counts.
	CountExact CountPrecision = "exact"
	// CountEstimated reports approximate term counts.
	CountEstimated CountPrecision = "estimated"
)

// Segment is untrusted text; UIs escape Text even when Matched is true.
type Segment struct {
	Text    string
	Matched bool
}

// Fragment groups adjacent highlight segments.
type Fragment struct {
	Segments []Segment
}

// Bucket counts documents, deduplicating repeated array terms within each document.
type Bucket struct {
	Value Value
	Count uint64
}

// FacetResult retains truncation separately from count precision.
type FacetResult struct {
	Name      string
	Field     FieldName
	Buckets   []Bucket
	Precision CountPrecision
	Truncated bool
}

// Hit is one document and its response-local ranking metadata.
type Hit struct {
	Document   Document
	Score      *float64
	SortValues []Value
	Highlights map[FieldName][]Fragment
}

// Result contains one complete backend search response.
type Result struct {
	Hits   []Hit
	Total  Total
	Facets []FacetResult
	Took   time.Duration
}

// HitOf is a typed hit whose document need not contain its identity.
type HitOf[T any] struct {
	ID         DocumentID
	Value      T
	Score      *float64
	SortValues []Value
	Highlights map[FieldName][]Fragment
}

// Page is a typed complete search response.
type Page[T any] struct {
	Hits   []HitOf[T]
	Total  Total
	Facets []FacetResult
	Took   time.Duration
}
