package main

import (
	"strings"
	"testing"

	"github.com/goforj/search/docs/gen"
)

// TestQuickStartRequiresTestedSource prevents generation from silently dropping the opening workflow.
func TestQuickStartRequiresTestedSource(t *testing.T) {
	if _, err := quickStartMarkdown([]gen.Example{{Name: "filters"}}); err == nil {
		t.Fatal("missing quick start was accepted")
	}
	program := "// Code generated from examples/quickstart_test.go; DO NOT EDIT.\npackage main\n\n// main executes the documented workflow.\nfunc main() {}\n"
	markdown, err := quickStartMarkdown([]gen.Example{{Name: "quickstart", Program: []byte(program)}})
	if err != nil || !strings.Contains(markdown, "package main") || !strings.Contains(markdown, "func main() {}") {
		t.Fatal(markdown, err)
	}
	if strings.Contains(markdown, "DO NOT EDIT") || strings.Contains(markdown, "// main executes") {
		t.Fatal("generator metadata leaked into the opening example")
	}
}
