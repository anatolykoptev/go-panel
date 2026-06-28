package components_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// allowedImportPrefixes is the set of allowed import prefixes for the
// components package. Only github.com/a-h/templ and stdlib are permitted.
// stdlib packages have no dot in the first path segment; templ is the sole
// third-party dependency.
var allowedImportPrefixes = []string{
	"github.com/a-h/templ",
}

// isStdlib reports whether an import path is a stdlib package.
// Stdlib packages never have a dot in their first path segment.
func isStdlib(importPath string) bool {
	first := strings.SplitN(importPath, "/", 2)[0]
	return !strings.Contains(first, ".")
}

// TestBoundary_ImportsSubsetOfTemplAndStdlib parses the components package
// source files and asserts that every non-test import is either stdlib or
// github.com/a-h/templ. This enforces the pure-presentation constraint: the
// package must not transitively depend on net/http, go-panel internals, or
// any other third-party module.
//
// Falsification: add `import "net/http"` to statcard.templ (the generated
// _templ.go) → this test fails naming the disallowed import.
func TestBoundary_ImportsSubsetOfTemplAndStdlib(t *testing.T) {
	fset := token.NewFileSet()
	dir := "."

	// Walk the directory and parse each non-test .go file individually.
	// Avoids parser.ParseDir which is deprecated since Go 1.25.
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && path != dir {
			return fs.SkipDir // only the top-level package directory
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Errorf("parse %q: %v", path, parseErr)
			return nil
		}
		for _, imp := range f.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			if isStdlib(impPath) {
				continue
			}
			allowed := false
			for _, prefix := range allowedImportPrefixes {
				if impPath == prefix || strings.HasPrefix(impPath, prefix+"/") {
					allowed = true
					break
				}
			}
			if !allowed {
				t.Errorf("file %q: disallowed import %q (only github.com/a-h/templ and stdlib permitted)",
					name, impPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", dir, err)
	}
}
