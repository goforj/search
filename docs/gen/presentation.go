package gen

import (
	"encoding/xml"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
)

// driverPresentation keeps documentation styling separate from executed support evidence.
type driverPresentation struct {
	label  string
	module string
	color  string
	icon   string
}

// driverPresentations maps tested engine names to readable labels, package links, and local icon assets.
var driverPresentations = map[string]driverPresentation{
	"bleve":         {"Bleve", "blevesearch", "156C4A", "bleve"},
	"meilisearch":   {"Meilisearch", "meilisearch", "A52C6F", "meilisearch"},
	"typesense":     {"Typesense", "typesensesearch", "9A3412", "typesense"},
	"opensearch":    {"OpenSearch", "opensearch", "005EB8", "opensearch"},
	"elasticsearch": {"Elasticsearch", "elasticsearch", "005571", "elasticsearch"},
	"postgres":      {"PostgreSQL FTS", "postgressearch", "336791", "postgresql"},
	"sqlite":        {"SQLite FTS5", "sqlitesearch", "0F6B8E", "sqlite"},
	"redis":         {"Redis Search", "redissearch", "B8272C", "redis"},
}

// driverBadge embeds local vector artwork so GitHub never needs to fetch a third-party badge or icon.
func driverBadge(root, name string) (string, error) {
	presentation, ok := driverPresentations[name]
	if !ok {
		return "", fmt.Errorf("missing driver presentation for %q", name)
	}
	source, err := os.ReadFile(filepath.Join(root, "docs/assets/icons", presentation.icon+".svg"))
	if err != nil {
		return "", err
	}
	var icon struct {
		ViewBox string `xml:"viewBox,attr"`
		Paths   []struct {
			D string `xml:"d,attr"`
		} `xml:"path"`
	}
	if err := xml.Unmarshal(source, &icon); err != nil {
		return "", err
	}
	if icon.ViewBox != "0 0 24 24" || len(icon.Paths) == 0 {
		return "", fmt.Errorf("invalid driver icon %q", presentation.icon)
	}
	var paths strings.Builder
	for _, path := range icon.Paths {
		fmt.Fprintf(&paths, `<path d="%s"/>`, html.EscapeString(path.D))
	}
	width := len(presentation.label)*7 + 40
	label := html.EscapeString(presentation.label)
	data := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="22" role="img" aria-label="%s">
  <title>%s</title>
  <rect width="%d" height="22" rx="4" fill="#%s"/>
  <g fill="#fff" transform="translate(7 3) scale(.666667)">%s</g>
  <text x="31" y="15" fill="#fff" font-family="Verdana,DejaVu Sans,sans-serif" font-size="11">%s</text>
</svg>
`, width, label, label, width, presentation.color, paths.String(), label)
	relative := "docs/assets/badges/driver-" + name + ".svg"
	absolute := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(absolute, []byte(data), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("[![%s](%s)](driver/%s)", presentation.label, relative, presentation.module), nil
}

// FeaturesMarkdown presents the portable library contract with links to its authoritative examples and APIs.
func FeaturesMarkdown() string {
	rows := []struct{ icon, name, capability, reference string }{
		{"search", "Literal search", "Require every analyzed term, with explicit searchable fields.", "[Typed search example](examples/search/main.go)"},
		{"filters", "Typed filters", "Combine Boolean logic, exact values, integer ranges, and array membership.", "[Filter example](examples/filters/main.go)"},
		{"sorting", "Stable sorting", "Order by up to two declared fields, followed by the document ID.", "[`Sort`](definition.go)"},
		{"pagination", "Pagination", "Use bounded offsets and limits with explicit total-count precision.", "[`Query`](query.go)"},
		{"facets", "Term facets", "Count matching documents before pagination, deduplicating repeated array terms.", "[Facet example](examples/facets/main.go)"},
		{"highlights", "Structured highlights", "Render untrusted text segments with explicit match markers.", "[Highlight example](examples/highlights/main.go)"},
		{"bulk", "Bulk operations", "Retain input positions and honest per-item or aggregate outcomes.", "[Bulk example](examples/bulk/main.go)"},
		{"receipts", "Visibility receipts", "Wait for accepted writes to become searchable and retain recovery evidence.", "[`Receipt` and `Outcome`](mutation.go)"},
		{"documents", "Typed documents", "Use typed Go values while keeping document IDs separate from JSON bodies.", "[Typed search example](examples/search/main.go)"},
		{"schemas", "Explicit schemas", "Provision deliberately and detect schema drift without runtime migration.", "[`Definition`](definition.go)"},
		{"observability", "Safe observability", "Observe classified events with panic isolation and optional bounded async delivery.", "[`Observer`](observer.go) · [`AsyncObserver`](observer_async.go)"},
		{"testing", "Isolated testing", "Script results and inspect recorded calls without running a search engine.", "[Fake backend example](examples/fake/main.go)"},
	}
	var out strings.Builder
	out.WriteString("| | Feature | What you get | Example or API |\n| :---: | --- | --- | --- |\n")
	for _, row := range rows {
		fmt.Fprintf(&out, "| <img src=\"docs/assets/icons/features/%s.svg\" width=\"20\" height=\"20\" alt=\"\"> | **%s** | %s | %s |\n", row.icon, row.name, row.capability, row.reference)
	}
	return out.String()
}
