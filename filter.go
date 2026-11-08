package search

import "slices"

// FilterKind identifies an immutable expression node.
type FilterKind string

const (
	// FilterAnd applies conjunction, with an empty conjunction matching all.
	FilterAnd FilterKind = "and"
	// FilterOr applies disjunction, with an empty disjunction matching none.
	FilterOr FilterKind = "or"
	// FilterNot complements its child's two-valued result.
	FilterNot FilterKind = "not"
	// FilterEqual compares an exact scalar.
	FilterEqual FilterKind = "equal"
	// FilterIn tests membership in a scalar set.
	FilterIn FilterKind = "in"
	// FilterContainsAny tests at least one array member.
	FilterContainsAny FilterKind = "contains_any"
	// FilterContainsAll tests every requested array member.
	FilterContainsAll FilterKind = "contains_all"
	// FilterLessThan compares integers exclusively.
	FilterLessThan FilterKind = "less_than"
	// FilterLessThanOrEqual compares integers inclusively.
	FilterLessThanOrEqual FilterKind = "less_than_or_equal"
	// FilterGreaterThan compares integers exclusively.
	FilterGreaterThan FilterKind = "greater_than"
	// FilterGreaterThanOrEqual compares integers inclusively.
	FilterGreaterThanOrEqual FilterKind = "greater_than_or_equal"
	// FilterBetween tests an inclusive integer interval.
	FilterBetween FilterKind = "between"
	// FilterExists tests presence of a non-null value, including an empty array.
	FilterExists FilterKind = "exists"
)

// Filter is an immutable expression. A zero filter means no predicate on Query only.
type Filter struct {
	node *filterNode
}

// filterNode prevents callers from creating inconsistent expression unions.
type filterNode struct {
	kind     FilterKind
	field    FieldName
	values   []Value
	children []Filter
}

// FilterView is a detached driver-facing expression view.
type FilterView struct {
	Kind     FilterKind
	Field    FieldName
	Values   []Value
	Children []Filter
}

// View returns cloned collections so compilers cannot alter the expression.
// @group Filters
func (f Filter) View() FilterView {
	if f.node == nil {
		return FilterView{}
	}
	return FilterView{f.node.kind, f.node.field, slices.Clone(f.node.values), slices.Clone(f.node.children)}
}

// leaf retains owned scalar arguments for later definition-aware validation.
func leaf(k FilterKind, f FieldName, v ...Value) Filter {
	return Filter{&filterNode{kind: k, field: f, values: slices.Clone(v)}}
}

// And combines predicates without allowing caller mutation of the child slice.
// @group Filters
func And(f ...Filter) Filter {
	return Filter{&filterNode{kind: FilterAnd, children: slices.Clone(f)}}
}

// Or combines alternatives, with zero alternatives matching nothing.
// @group Filters
func Or(f ...Filter) Filter {
	return Filter{&filterNode{kind: FilterOr, children: slices.Clone(f)}}
}

// Not complements a predicate; a zero child fails query validation.
// @group Filters
func Not(f Filter) Filter {
	return Filter{&filterNode{kind: FilterNot, children: []Filter{f}}}
}

// Equal tests scalar equality; absent and null values compare false.
// @group Filters
func Equal(f FieldName, v Value) Filter {
	return leaf(FilterEqual, f, v)
}

// In tests whether a scalar equals one of a nonempty set.
// @group Filters
func In(f FieldName, v ...Value) Filter {
	return leaf(FilterIn, f, v...)
}

// ContainsAny tests array intersection with a nonempty set.
// @group Filters
func ContainsAny(f FieldName, v ...Value) Filter {
	return leaf(FilterContainsAny, f, v...)
}

// ContainsAll tests whether an array contains a nonempty set.
// @group Filters
func ContainsAll(f FieldName, v ...Value) Filter {
	return leaf(FilterContainsAll, f, v...)
}

// Exists tests non-null presence, independently of array membership.
// @group Filters
func Exists(f FieldName) Filter {
	return leaf(FilterExists, f)
}

// LessThan compares an integer against an exclusive upper bound.
// @group Filters
func LessThan(f FieldName, v Value) Filter {
	return leaf(FilterLessThan, f, v)
}

// LessThanOrEqual compares an integer against an inclusive upper bound.
// @group Filters
func LessThanOrEqual(f FieldName, v Value) Filter {
	return leaf(FilterLessThanOrEqual, f, v)
}

// GreaterThan compares an integer against an exclusive lower bound.
// @group Filters
func GreaterThan(f FieldName, v Value) Filter {
	return leaf(FilterGreaterThan, f, v)
}

// GreaterThanOrEqual compares an integer against an inclusive lower bound.
// @group Filters
func GreaterThanOrEqual(f FieldName, v Value) Filter {
	return leaf(FilterGreaterThanOrEqual, f, v)
}

// Between tests an inclusive, ordered integer range.
// @group Filters
func Between(f FieldName, a, b Value) Filter {
	return leaf(FilterBetween, f, a, b)
}
