package search

// Limits bounds work before it reaches a backend. Zero option fields inherit defaults.
type Limits struct {
	NameBytes        int
	DefinitionBytes  int
	DocumentBytes    int
	JSONDepth        int
	ObjectMembers    int
	ArrayLength      int
	DefinitionFields int
	JSONPointerDepth int
	TextBytes        int
	QueryBytes       int
	QueryTerms       int
	QueryFields      int
	FilterNodes      int
	FilterDepth      int
	FilterValues     int
	FilterValueBytes int
	FacetNameBytes   int
	ResultLimit      uint64
	ResultWindow     uint64
	Facets           int
	HighlightFields  int
	BulkDocuments    int
	BulkBytes        int
}

// DefaultLimits returns conservative allocation and backend-work budgets.
// @group Construction
func DefaultLimits() Limits {
	return Limits{
		NameBytes:        128,
		DefinitionBytes:  64 << 10,
		DocumentBytes:    1 << 20,
		JSONDepth:        32,
		ObjectMembers:    4096,
		ArrayLength:      4096,
		DefinitionFields: 128,
		JSONPointerDepth: 16,
		TextBytes:        256 << 10,
		QueryBytes:       16 << 10,
		QueryTerms:       64,
		QueryFields:      32,
		FilterNodes:      128,
		FilterDepth:      16,
		FilterValues:     256,
		FilterValueBytes: 32 << 10,
		FacetNameBytes:   128,
		ResultLimit:      100,
		ResultWindow:     1000,
		Facets:           16,
		HighlightFields:  16,
		BulkDocuments:    1000,
		BulkBytes:        8 << 20,
	}
}

// mergeLimits refuses negative values and ceilings above sixteen times the defaults.
func mergeLimits(over Limits) (Limits, error) {
	d := DefaultLimits()
	overrides := []struct {
		target *int
		value  int
	}{
		{&d.NameBytes, over.NameBytes},
		{&d.DefinitionBytes, over.DefinitionBytes},
		{&d.DocumentBytes, over.DocumentBytes},
		{&d.JSONDepth, over.JSONDepth},
		{&d.ObjectMembers, over.ObjectMembers},
		{&d.ArrayLength, over.ArrayLength},
		{&d.DefinitionFields, over.DefinitionFields},
		{&d.JSONPointerDepth, over.JSONPointerDepth},
		{&d.TextBytes, over.TextBytes},
		{&d.QueryBytes, over.QueryBytes},
		{&d.QueryTerms, over.QueryTerms},
		{&d.QueryFields, over.QueryFields},
		{&d.FilterNodes, over.FilterNodes},
		{&d.FilterDepth, over.FilterDepth},
		{&d.FilterValues, over.FilterValues},
		{&d.FilterValueBytes, over.FilterValueBytes},
		{&d.FacetNameBytes, over.FacetNameBytes},
		{&d.Facets, over.Facets},
		{&d.HighlightFields, over.HighlightFields},
		{&d.BulkDocuments, over.BulkDocuments},
		{&d.BulkBytes, over.BulkBytes},
	}
	for _, entry := range overrides {
		if entry.value < 0 || entry.value > *entry.target*16 {
			return Limits{}, ErrInvalidDefinition
		}
		if entry.value != 0 {
			*entry.target = entry.value
		}
	}
	if over.ResultLimit > 1600 || over.ResultWindow > 16000 {
		return Limits{}, ErrInvalidDefinition
	}
	if over.ResultLimit != 0 {
		d.ResultLimit = over.ResultLimit
	}
	if over.ResultWindow != 0 {
		d.ResultWindow = over.ResultWindow
	}
	if d.ResultLimit > d.ResultWindow {
		return Limits{}, ErrInvalidDefinition
	}
	return d, nil
}
