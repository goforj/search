package redissearch

import (
	"strconv"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// integer renders only root-validated exact integer scalars in native range syntax.
func integer(value search.Value) string {
	n, _ := value.Integer()
	return strconv.FormatInt(n, 10)
}

// compileFilter uses encoded TAG terms and explicit universe anchoring for two-valued negation.
func compileFilter(filter search.Filter) string {
	view := filter.View()
	key := "@" + driverutil.FieldKey(view.Field)
	switch view.Kind {
	case "":
		return "*"
	case search.FilterAnd, search.FilterOr:
		children := []string{}
		for _, child := range view.Children {
			value := compileFilter(child)
			if view.Kind == search.FilterAnd && value == noMatches {
				return noMatches
			}
			if view.Kind == search.FilterOr && value == "*" {
				return "*"
			}
			if value == "*" || value == noMatches {
				continue
			}
			children = append(children, "("+value+")")
		}
		if len(children) == 0 {
			if view.Kind == search.FilterAnd {
				return "*"
			}
			return noMatches
		}
		join := " "
		if view.Kind == search.FilterOr {
			join = " | "
		}
		return "(" + strings.Join(children, join) + ")"
	case search.FilterNot:
		child := compileFilter(view.Children[0])
		if child == "*" {
			return noMatches
		}
		if child == noMatches {
			return "*"
		}
		return "-(" + child + ")"
	case search.FilterExists:
		return key + "_exists:{1}"
	case search.FilterEqual:
		if view.Values[0].Kind() == search.IntegerValue {
			value := integer(view.Values[0])
			return key + ":[" + value + " " + value + "]"
		}
		return key + ":{" + driverutil.Scalar(view.Values[0]) + "}"
	case search.FilterIn, search.FilterContainsAny, search.FilterContainsAll:
		children := make([]string, len(view.Values))
		for n, value := range view.Values {
			if value.Kind() == search.IntegerValue {
				number := integer(value)
				children[n] = key + ":[" + number + " " + number + "]"
			} else {
				children[n] = key + ":{" + driverutil.Scalar(value) + "}"
			}
		}
		join := " | "
		if view.Kind == search.FilterContainsAll {
			join = " "
		}
		return "(" + strings.Join(children, join) + ")"
	case search.FilterBetween:
		return key + ":[" + integer(view.Values[0]) + " " + integer(view.Values[1]) + "]"
	case search.FilterLessThan:
		return key + ":[-inf (" + integer(view.Values[0]) + "]"
	case search.FilterLessThanOrEqual:
		return key + ":[-inf " + integer(view.Values[0]) + "]"
	case search.FilterGreaterThan:
		return key + ":[(" + integer(view.Values[0]) + " +inf]"
	default:
		return key + ":[" + integer(view.Values[0]) + " +inf]"
	}
}

// noMatches uses an impossible encoded ID rather than invalid negated-wildcard syntax.
const noMatches = "@_forjid:{none}"
