// Package gen derives public documentation from GoDoc and executable examples.
package gen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Symbol retains the qualified package and receiver identity of a public declaration.
type Symbol struct {
	Name        string
	Package     string
	Anchor      string
	Group       string
	Description string
	Source      string
	Line        int
}

// Example describes a runnable program and its documentation annotations.
type Example struct {
	Name    string
	Title   string
	Source  string
	Program []byte
	APIs    []string
	Snippet string
}

// Root locates the library rather than accidentally selecting the tooling module.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/goforj/search\n") {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("search module root not found")
		}
		dir = parent
	}
}

// Symbols inventories root and qualified driver APIs without importing their dependencies.
func Symbols(root string) ([]Symbol, error) {
	targets := []string{".", "driver/blevesearch", "driver/meilisearch", "driver/typesensesearch", "driver/opensearch", "driver/elasticsearch", "driver/postgressearch", "driver/sqlitesearch", "driver/redissearch", "searchfake", "searchtest"}
	var symbols []Symbol
	for _, target := range targets {
		set := token.NewFileSet()
		packages, err := parser.ParseDir(set, filepath.Join(root, target), func(info os.FileInfo) bool { return !strings.HasSuffix(info.Name(), "_test.go") }, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, pkg := range packages {
			for filename, file := range pkg.Files {
				group := fileGroup(filepath.Base(filename))
				if target != "." {
					group = "Backend"
				}
				if strings.HasPrefix(target, "search") {
					group = "Testing"
				}
				add := func(name, anchor string, doc *ast.CommentGroup, requireGroup bool) error {
					if doc == nil {
						return fmt.Errorf("missing GoDoc: %s.%s", pkg.Name, name)
					}
					text := doc.Text()
					selected := group
					found := false
					for _, line := range strings.Split(text, "\n") {
						if value, ok := strings.CutPrefix(strings.TrimSpace(line), "@group "); ok {
							selected, found = value, true
						}
					}
					if requireGroup && !found {
						return fmt.Errorf("missing @group: %s.%s", pkg.Name, name)
					}
					description := strings.TrimSpace(strings.Split(text, "\n")[0])
					path := "github.com/goforj/search"
					if target != "." {
						path += "/" + target
					}
					symbols = append(symbols, Symbol{Name: pkg.Name + "." + name, Package: path, Anchor: anchor, Group: selected, Description: description, Source: filepath.ToSlash(filepath.Join(target, filepath.Base(filename))), Line: set.Position(doc.End()).Line + 1})
					return nil
				}
				for _, decl := range file.Decls {
					switch node := decl.(type) {
					case *ast.FuncDecl:
						if !ast.IsExported(node.Name.Name) {
							continue
						}
						name := node.Name.Name
						if node.Recv != nil {
							receiver := receiverName(node.Recv.List[0].Type)
							if !ast.IsExported(receiver) {
								continue
							}
							name = receiver + "." + name
						}
						if err := add(name, name, node.Doc, true); err != nil {
							return nil, err
						}
					case *ast.GenDecl:
						for _, spec := range node.Specs {
							switch item := spec.(type) {
							case *ast.TypeSpec:
								if ast.IsExported(item.Name.Name) {
									doc := item.Doc
									if doc == nil {
										doc = node.Doc
									}
									if err := add(item.Name.Name, item.Name.Name, doc, false); err != nil {
										return nil, err
									}
								}
							case *ast.ValueSpec:
								for _, name := range item.Names {
									if ast.IsExported(name.Name) {
										doc := item.Doc
										if doc == nil {
											doc = node.Doc
										}
										if err := add(name.Name, name.Name, doc, false); err != nil {
											return nil, err
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
	sort.Slice(symbols, func(i, j int) bool {
		if symbols[i].Group != symbols[j].Group {
			return symbols[i].Group < symbols[j].Group
		}
		return symbols[i].Name < symbols[j].Name
	})
	return symbols, nil
}

// receiverName unwraps pointers and generic syntax without losing receiver ownership.
func receiverName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.StarExpr:
		return receiverName(node.X)
	case *ast.IndexExpr:
		return receiverName(node.X)
	case *ast.IndexListExpr:
		return receiverName(node.X)
	}
	return ""
}

// fileGroup keeps type declarations close to their reader-facing API functions.
func fileGroup(filename string) string {
	groups := map[string]string{
		"definition.go": "Definitions", "document.go": "Documents", "filter.go": "Filters",
		"value.go": "Values", "mutation.go": "Receipts", "query.go": "Queries", "result.go": "Results",
		"observer.go": "Observability", "observer_async.go": "Observability", "index.go": "Construction", "limits.go": "Construction", "errors.go": "Backend",
	}
	return groups[filename]
}

// Examples reads documentation directly from the runnable programs tested by the example runner.
func Examples(root string) ([]Example, error) {
	files, err := filepath.Glob(filepath.Join(root, "examples/*/main.go"))
	if err != nil {
		return nil, err
	}
	var examples []Example
	for _, filename := range files {
		data, err := os.ReadFile(filename)
		if err != nil {
			return nil, err
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, filename, data, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "main" {
				continue
			}
			name := filepath.Base(filepath.Dir(filename))
			title := name
			var apis []string
			if fn.Doc == nil {
				return nil, fmt.Errorf("example %s: missing main documentation", name)
			}
			for _, line := range strings.Split(fn.Doc.Text(), "\n") {
				if value, ok := strings.CutPrefix(strings.TrimSpace(line), "@readme "); ok {
					title = value
				}
				if value, ok := strings.CutPrefix(strings.TrimSpace(line), "@api "); ok {
					apis = append(apis, strings.Fields(value)...)
				}
			}
			start, end := set.Position(fn.Body.Lbrace).Offset+1, set.Position(fn.Body.Rbrace).Offset
			snippet, err := exampleSnippet(string(data[start:end]), len(apis) > 0)
			if err != nil {
				return nil, fmt.Errorf("example %s: %w", name, err)
			}
			source, err := filepath.Rel(filepath.Join(root, "examples"), filename)
			if err != nil {
				return nil, err
			}
			examples = append(examples, Example{Name: name, Title: title, Source: filepath.ToSlash(source), Program: data, APIs: apis, Snippet: snippet})
		}
	}
	sort.Slice(examples, func(i, j int) bool { return examples[i].Name < examples[j].Name })
	return examples, nil
}

// exampleSnippet extracts a complete marked region without copying setup into every API example.
func exampleSnippet(body string, required bool) (string, error) {
	const start, end = "// snippet:start\n", "// snippet:end"
	if !required && !strings.Contains(body, start) && !strings.Contains(body, end) {
		return "", nil
	}
	if strings.Count(body, start) != 1 || strings.Count(body, end) != 1 {
		return "", fmt.Errorf("expected one snippet marker pair")
	}
	left, right := strings.Index(body, start)+len(start), strings.Index(body, end)
	if right < left {
		return "", fmt.Errorf("reversed snippet markers")
	}
	if strings.TrimSpace(body[left:right]) == "" {
		return "", fmt.Errorf("empty example snippet")
	}
	lines := strings.Split(strings.Trim(body[left:right], "\n\t "), "\n")
	for n := 1; n < len(lines); n++ {
		lines[n] = strings.TrimPrefix(lines[n], "\t")
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// Replace updates one exact generated region and rejects missing or ambiguous markers.
func Replace(source, name, body string) (string, error) {
	start, end := "<!-- "+name+":embed:start -->", "<!-- "+name+":embed:end -->"
	if strings.Count(source, start) != 1 || strings.Count(source, end) != 1 {
		return "", fmt.Errorf("expected one %s marker pair", name)
	}
	left, right := strings.Index(source, start), strings.Index(source, end)
	if right < left {
		return "", fmt.Errorf("reversed %s markers", name)
	}
	return source[:left+len(start)] + "\n" + strings.TrimSpace(body) + "\n" + source[right:], nil
}

// SourceFingerprint binds driver evidence to library code, module pins, and executable integration fixtures.
func SourceFingerprint(root string) (string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if relative == ".git" || relative == "docs" || relative == "examples" || relative == ".artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") || entry.Name() == "go.mod" || filepath.ToSlash(relative) == "integration/versions.json" {
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	hash := sha256.New()
	for _, name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00", filepath.ToSlash(name))
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
