package sqlsearch

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/goforj/search"
	"github.com/goforj/search/internal/driverutil"
)

// fieldKey bounds identifiers below both SQL engines' limits independently of logical name length.
func fieldKey(name search.FieldName) string {
	hash := sha256.Sum256([]byte(name))
	return "f" + hex.EncodeToString(hash[:8])
}

// compiler owns positional parameters for a single query family.
type compiler struct {
	driver *Driver
	args   []any
}

// parameter prevents application values from becoming executable SQL.
func (c *compiler) parameter(value any) string {
	c.args = append(c.args, value)
	return c.driver.bind(len(c.args))
}

// filter compiles two-valued predicates, including missing/null complements and empty conjunctions.
func (c *compiler) filter(filter search.Filter) string {
	view := filter.View()
	key := "d." + fieldKey(view.Field)
	switch view.Kind {
	case "":
		return "1=1"
	case search.FilterAnd, search.FilterOr:
		identity, join := "1=1", " AND "
		if view.Kind == search.FilterOr {
			identity, join = "1=0", " OR "
		}
		if len(view.Children) == 0 {
			return identity
		}
		children := make([]string, len(view.Children))
		for n, child := range view.Children {
			children[n] = c.filter(child)
		}
		return "(" + strings.Join(children, join) + ")"
	case search.FilterNot:
		return "NOT (" + c.filter(view.Children[0]) + ")"
	case search.FilterExists:
		return key + "_exists IS NOT NULL"
	case search.FilterContainsAny, search.FilterContainsAll:
		children := make([]string, len(view.Values))
		for n, value := range view.Values {
			children[n] = "EXISTS (SELECT 1 FROM " + c.driver.name + "_values v WHERE v.owner=d.owner AND v.field=" + c.parameter(fieldKey(view.Field)) + " AND v.value=" + c.parameter(driverutil.Scalar(value)) + ")"
		}
		join := " OR "
		if view.Kind == search.FilterContainsAll {
			join = " AND "
		}
		return "(" + strings.Join(children, join) + ")"
	case search.FilterIn:
		values := make([]string, len(view.Values))
		for n, value := range view.Values {
			values[n] = c.parameter(driverutil.Scalar(value))
		}
		return "coalesce(" + key + " IN (" + strings.Join(values, ",") + "),false)"
	case search.FilterBetween:
		return "coalesce(" + key + " BETWEEN " + c.parameter(driverutil.Scalar(view.Values[0])) + " AND " + c.parameter(driverutil.Scalar(view.Values[1])) + ",false)"
	default:
		operators := map[search.FilterKind]string{search.FilterEqual: "=", search.FilterLessThan: "<", search.FilterLessThanOrEqual: "<=", search.FilterGreaterThan: ">", search.FilterGreaterThanOrEqual: ">="}
		return "coalesce(" + key + operators[view.Kind] + c.parameter(driverutil.Scalar(view.Values[0])) + ",false)"
	}
}
