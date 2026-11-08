package examples_test

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExamples executes the programs users run and verifies their documented output.
func TestExamples(t *testing.T) {
	files, err := filepath.Glob("*/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no runnable examples found")
	}
	for _, filename := range files {
		t.Run(filepath.Dir(filename), func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			var expected string
			outputs := 0
			for _, group := range file.Comments {
				if output, ok := strings.CutPrefix(group.Text(), "Output:\n"); ok {
					expected = output
					outputs++
				}
			}
			if outputs != 1 {
				t.Fatal("expected exactly one Output: comment")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			args := []string{"run"}
			if exampleRace {
				args = append(args, "-race")
			}
			coverage := os.Getenv("SEARCH_EXAMPLE_COVERAGE_DIR")
			if coverage != "" {
				args = append(args, "-cover", "-coverpkg=github.com/goforj/search/...")
			}
			args = append(args, "./"+filepath.Dir(filename))
			cmd := exec.CommandContext(ctx, "go", args...)
			cmd.Env = append(os.Environ(), "GOWORK=off")
			if coverage != "" {
				cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverage)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("run example: %v\n%s", err, stderr.String())
			}
			if string(output) != expected {
				t.Fatalf("output mismatch:\ngot: %s\nwant: %s", output, expected)
			}
		})
	}
}
