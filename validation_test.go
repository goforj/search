package search

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

// testDefinition exercises every portable field shape and duplicate-path mappings.
func testDefinition() Definition {
	return Definition{Name: "products", Fields: []Field{
		{Name: "title", Type: TextField, Searchable: true},
		{Name: "code", Type: KeywordField, Filterable: true, Sortable: true, Required: true, Facetable: true},
		{Name: "price", Type: IntegerField, Filterable: true, Sortable: true, Required: true},
		{Name: "active", Type: BooleanField, Filterable: true, Facetable: true},
		{Name: "tags", Type: KeywordsField, Filterable: true, Facetable: true},
	}}
}

// TestDefinitionValidation covers invalid names, mappings, defaults, and the portable type matrix.
func TestDefinitionValidation(t *testing.T) {
	cases := map[string]func(*Definition){
		"empty-name":               func(d *Definition) { d.Name = "" },
		"reserved-name":            func(d *Definition) { d.Name = "_forj_private" },
		"traversal":                func(d *Definition) { d.Name = "../outside" },
		"control":                  func(d *Definition) { d.Name = "a\nb" },
		"invalid-utf8":             func(d *Definition) { d.Name = IndexName(string([]byte{255})) },
		"empty-fields":             func(d *Definition) { d.Fields = nil },
		"duplicate-field":          func(d *Definition) { d.Fields = append(d.Fields, d.Fields[0]) },
		"invalid-field-name":       func(d *Definition) { d.Fields[0].Name = "bad.field" },
		"invalid-type":             func(d *Definition) { d.Fields[0].Type = "unknown" },
		"invalid-path":             func(d *Definition) { d.Fields[0].Path = "not/pointer" },
		"bad-pointer-escape":       func(d *Definition) { d.Fields[0].Path = "/bad~2" },
		"trailing-pointer-escape":  func(d *Definition) { d.Fields[0].Path = "/bad~" },
		"reserved-path":            func(d *Definition) { d.Fields[0].Path = "/_forj_schema" },
		"text-filter":              func(d *Definition) { d.Fields[0].Filterable = true },
		"text-sort":                func(d *Definition) { d.Fields[0].Sortable = true },
		"text-facet":               func(d *Definition) { d.Fields[0].Facetable = true },
		"keyword-search":           func(d *Definition) { d.Fields[1].Searchable = true },
		"integer-search":           func(d *Definition) { d.Fields[2].Searchable = true },
		"integer-facet":            func(d *Definition) { d.Fields[2].Facetable = true },
		"boolean-search":           func(d *Definition) { d.Fields[3].Searchable = true },
		"array-search":             func(d *Definition) { d.Fields[4].Searchable = true },
		"array-sort":               func(d *Definition) { d.Fields[4].Sortable = true },
		"optional-sort":            func(d *Definition) { d.Fields[1].Required = false },
		"unknown-search-default":   func(d *Definition) { d.DefaultSearchable = []FieldName{"absent"} },
		"duplicate-search-default": func(d *Definition) { d.DefaultSearchable = []FieldName{"title", "title"} },
		"invalid-sort-default":     func(d *Definition) { d.DefaultSort = []Sort{{Field: "code"}} },
		"duplicate-sort-default":   func(d *Definition) { d.DefaultSort = []Sort{{"code", Ascending}, {"code", Descending}} },
		"too-many-default-sorts": func(d *Definition) {
			d.DefaultSort = []Sort{{"code", Ascending}, {"price", Ascending}, {"code", Ascending}}
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			d := testDefinition()
			edit(&d)
			if err := d.Validate(); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("got %v", err)
			}
		})
	}
	d := testDefinition()
	first, err := d.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	d.Fields[0], d.Fields[1] = d.Fields[1], d.Fields[0]
	second, err := d.Fingerprint()
	if err != nil || first != second {
		t.Fatal("unstable fingerprint", err)
	}
}

// TestDocumentValidation covers malformed JSON, structural budgets, scalar types, and integer boundaries.
func TestDocumentValidation(t *testing.T) {
	def, err := normalizeDefinition(testDefinition(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		`null`, `[]`, `true`, `{`, `{"code":"a","price":1,"bad":"\ud800"}`, `{"code":"a","price":1,"bad":"\udc00"}`, `{"code":"a","price":1,"bad":"\ud800x"}`, `{} {}`, `{"code":"a","code":"b","price":1}`,
		`{"code":"a","price":null}`, `{"code":null,"price":1}`, `{"code":"a"}`,
		`{"code":3,"price":1}`, `{"code":"a","price":"1"}`, `{"code":"a","price":1.5}`,
		`{"code":"a","price":9007199254740992}`, `{"code":"a","price":-9007199254740992}`,
		`{"code":"a","price":1,"tags":[null]}`, `{"code":"a","price":1,"tags":[1]}`,
		`{"code":"a","price":1,"tags":"x"}`, `{"code":"a","price":1,"active":"true"}`,
		`{"code":"a","price":1,"title":false}`, `{"code":"a","price":1,"_forjbody":true}`,
		`{"code":"a","price":1,"nested":{"_forj":true}}`, `{"code":"a","price":1,"n":1e99999}`,
		"{\"code\":\"\xff\",\"price\":1}",
	}
	for n, body := range invalid {
		if _, _, err := normalizeDocument(Document{ID: "id", Body: json.RawMessage(body)}, def, DefaultLimits()); !errors.Is(err, ErrInvalidDocument) {
			t.Errorf("case %d accepted: %v", n, err)
		}
	}
	for _, body := range []string{
		`{"code":"a","price":1,"unicode":"\ud83d\ude00","literal":"\\ud800"}`,
		`{"code":"a","price":9007199254740991,"active":null,"tags":[],"unknown":{"x":[1]}}`,
		`{"code":"a","price":-9007199254740991,"title":"é","tags":["one","one"]}`,
	} {
		if _, _, err := normalizeDocument(Document{ID: "id", Body: json.RawMessage(body)}, def, DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []DocumentID{"", "bad\n", DocumentID(strings.Repeat("a", 129))} {
		if _, _, err := normalizeDocument(Document{ID: id, Body: json.RawMessage(`{}`)}, Definition{}, DefaultLimits()); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	for name, limits := range map[string]Limits{
		"bytes": {DocumentBytes: 1}, "depth": {JSONDepth: 1}, "members": {ObjectMembers: 1}, "array": {ArrayLength: 1}, "text": {TextBytes: 1},
	} {
		t.Run(name, func(t *testing.T) {
			l, err := mergeLimits(limits)
			if err != nil {
				t.Fatal(err)
			}
			body := json.RawMessage(`{"code":"long","price":1,"tags":["a","b"],"object":{"deep":{"v":1}}}`)
			if _, _, err := normalizeDocument(Document{ID: "id", Body: body}, def, l); err == nil {
				t.Fatal("budget ignored")
			}
		})
	}
}

// TestQueryValidation covers zero atoms, field permissions, expression arity, and overflow-safe windows.
func TestQueryValidation(t *testing.T) {
	d, err := normalizeDefinition(testDefinition(), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	keyword, _ := Keyword("one")
	one, _ := Integer(1)
	two, _ := Integer(2)
	invalid := []Query{
		{Text: string([]byte{255})}, {Text: strings.Repeat("x", 17000)}, {Text: strings.Repeat("x-", 65)},
		{Fields: []FieldName{"code"}}, {Fields: []FieldName{"missing"}}, {Fields: []FieldName{"title", "title"}},
		{Sort: []Sort{{"title", Ascending}}}, {Sort: []Sort{{"code", "wrong"}}},
		{Limit: 101}, {Offset: math.MaxUint64}, {Offset: 999},
		{Filter: Equal("code", Value{})}, {Filter: Equal("code", one)},
		{Filter: Equal("price", keyword)}, {Filter: Equal("active", keyword)},
		{Filter: Equal("tags", keyword)}, {Filter: ContainsAny("code", keyword)},
		{Filter: Equal("title", keyword)}, {Filter: Equal("missing", keyword)},
		{Filter: In("code")}, {Filter: ContainsAny("tags")}, {Filter: ContainsAll("tags")},
		{Filter: Not(Filter{})}, {Filter: And(Filter{})},
		{Filter: LessThan("code", keyword)}, {Filter: Between("price", two, one)},
		{Highlight: []Highlight{{Field: "code"}}}, {Highlight: []Highlight{{Field: "title"}, {Field: "title"}}},
		{Facets: []TermFacet{{Name: "", Field: "code"}}}, {Facets: []TermFacet{{Name: "prices", Field: "price"}}},
		{Facets: []TermFacet{{Name: "codes", Field: "code", Limit: 101}}},
		{Facets: []TermFacet{{Name: "codes", Field: "code"}, {Name: "codes", Field: "code"}}},
	}
	for n, q := range invalid {
		if _, err := normalizeQuery(q, d, DefaultLimits()); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("case %d accepted: %v", n, err)
		}
	}
	valid := []Filter{And(), Or(), Equal("code", keyword), In("code", keyword), ContainsAny("tags", keyword), ContainsAll("tags", keyword), Exists("code"), Not(Equal("code", keyword)), Between("price", one, two), LessThan("price", one), LessThanOrEqual("price", one), GreaterThan("price", one), GreaterThanOrEqual("price", one), Equal("active", Boolean(true))}
	for n, f := range valid {
		if _, err := normalizeQuery(Query{Filter: f}, d, DefaultLimits()); err != nil {
			t.Errorf("valid case %d: %v", n, err)
		}
	}
	if q, err := normalizeQuery(Query{}, d, DefaultLimits()); err != nil || q.Limit != 20 {
		t.Fatal(q, err)
	}
}

// TestImmutableValues verifies constructors reject invalid scalar unions and clone external slices.
func TestImmutableValues(t *testing.T) {
	if _, err := Integer(MaxInteger + 1); err == nil {
		t.Fatal("oversized integer accepted")
	}
	if _, err := Integer(-MaxInteger - 1); err == nil {
		t.Fatal("undersized integer accepted")
	}
	if _, err := Keyword(string([]byte{255})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	v, _ := Keyword("red")
	values := []Value{v}
	f := In("code", values...)
	values[0] = Boolean(true)
	view := f.View()
	view.Values[0] = Boolean(true)
	if text, ok := f.View().Values[0].Keyword(); !ok || text != "red" {
		t.Fatal("filter aliases input")
	}
	if _, ok := v.Integer(); ok {
		t.Fatal("invalid discriminator")
	}
	if b, ok := Boolean(true).Boolean(); !ok || !b {
		t.Fatal("Boolean accessor")
	}
	n, _ := Integer(3)
	if value, ok := n.Integer(); !ok || value != 3 {
		t.Fatal("integer accessor")
	}
}

// FuzzDocumentValidation proves bounded parsing never panics on malformed input.
func FuzzDocumentValidation(f *testing.F) {
	for _, seed := range []string{`{}`, `{"a":[true,null]}`, `{"a":1,"a":2}`, `{"a":"é"}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, body string) {
		if len(body) > 4096 {
			return
		}
		_, _, _ = normalizeDocument(Document{ID: "fuzz", Body: json.RawMessage(body)}, Definition{}, DefaultLimits())
	})
}

// BenchmarkDocumentValidation measures canonicalization without a backend or test fixture setup.
func BenchmarkDocumentValidation(b *testing.B) {
	d := Document{ID: "one", Body: json.RawMessage(`{"title":"search","code":"one","price":42}`)}
	def, err := normalizeDefinition(testDefinition(), DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := normalizeDocument(d, def, DefaultLimits()); err != nil {
			b.Fatal(err)
		}
	}
}

// TestNestedJSONPointers distinguishes escaped object keys, array indexes, and missing required values.
func TestNestedJSONPointers(t *testing.T) {
	definition := Definition{Name: "nested", Fields: []Field{{Name: "code", Path: "/nested/a~1b/0/~0key", Type: KeywordField, Required: true}}}
	normalized, err := normalizeDefinition(definition, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"nested":{"a/b":[{"~key":"one"}]}}`, `{"nested":{"a/b":[{"~key":"two"},{}]}}`} {
		if _, _, err := normalizeDocument(Document{ID: "one", Body: json.RawMessage(body)}, normalized, DefaultLimits()); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"nested":{"a/b":[]}}`, `{"nested":{"a/b":true}}`, `{"nested":{"a/b":[{}]}}`} {
		if _, _, err := normalizeDocument(Document{ID: "one", Body: json.RawMessage(body)}, normalized, DefaultLimits()); err == nil {
			t.Fatal("missing required nested value accepted")
		}
	}
	definition.Fields[0].Path = "/nested/00"
	normalized, err = normalizeDefinition(definition, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := normalizeDocument(Document{ID: "one", Body: json.RawMessage(`{"nested":["zero"]}`)}, normalized, DefaultLimits()); err == nil {
		t.Fatal("noncanonical array index accepted")
	}
}

// TestValueJSONPreservesScalarTypes prevents result DTOs from silently serializing private value fields as empty objects.
func TestValueJSONPreservesScalarTypes(t *testing.T) {
	keyword, err := Keyword("blue")
	if err != nil {
		t.Fatal(err)
	}
	integer, err := Integer(MaxInteger)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		value Value
		json  string
	}{
		{keyword, `"blue"`}, {Boolean(false), `false`}, {integer, `9007199254740991`},
	} {
		data, err := json.Marshal(test.value)
		if err != nil || string(data) != test.json {
			t.Fatal(string(data), err)
		}
	}
	if _, err := json.Marshal(Value{}); !errors.Is(err, ErrInvalidQuery) {
		t.Fatal(err)
	}
}

// TestTerminalRejectionCannotBecomeSuccess preserves already-proven submission failures through terminal inspection.
func TestTerminalRejectionCannotBecomeSuccess(t *testing.T) {
	submitted := []ItemResult{{ID: "one", Status: ItemRejected}}
	for _, test := range []struct {
		status ItemStatus
		valid  bool
	}{{ItemSucceeded, false}, {ItemUnknown, false}, {ItemFailed, true}, {ItemRejected, true}} {
		outcome := Outcome{State: OutcomeFailed, Bulk: &BulkOutcome{Precision: PerItem, Items: []ItemResult{{ID: "one", Status: test.status}}}}
		if validBulkOutcome(outcome, submitted) != test.valid {
			t.Fatal(test.status)
		}
	}
}

// TestPointerDepthRejectsBeforeSplit keeps excessive path components from allocating an unbounded slice.
func TestPointerDepthRejectsBeforeSplit(t *testing.T) {
	pointer := JSONPointer(strings.Repeat("/part", 65536))
	if _, ok := pointerParts(pointer, 16); ok {
		t.Fatal("over-depth pointer accepted")
	}
	allocations := testing.AllocsPerRun(10, func() { _, _ = pointerParts(pointer, 16) })
	if allocations != 0 {
		t.Fatalf("rejected pointer caused %.0f allocations", allocations)
	}
}
