package mcp

// The MCP server used to advertise the literal "0.1.0" while the module was at
// v0.22.2 — a hand-maintained second copy of a number release-please already
// owns. Nothing referenced it, so nothing went red as it drifted through
// twenty-two releases. These two tests pin the property that would have caught
// it: the version the server reports must be READ, never typed.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// Falsification: change the final `return "(devel)"` in moduleVersion (mcp.go)
// to `return "0.1.0"` — that is the branch a test binary takes — and this goes
// RED, because the returned string stops matching what the toolchain recorded.
func TestModuleVersionComesFromBuildInfo(t *testing.T) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		t.Skip("no build info in this binary")
	}

	// Under `go test` go-panel is the main module, so the toolchain has no
	// released version to stamp and moduleVersion must say so rather than
	// inventing one.
	var want string
	for _, d := range bi.Deps {
		if d.Path == modulePath {
			want = d.Version
			if d.Replace != nil && d.Replace.Version != "" {
				want = d.Replace.Version
			}
		}
	}
	if want == "" {
		if bi.Main.Path == modulePath && bi.Main.Version != "" {
			want = bi.Main.Version
		} else {
			want = "(devel)"
		}
	}

	if got := moduleVersion(); got != want {
		t.Fatalf("moduleVersion() = %q, build info says %q", got, want)
	}
	t.Logf("moduleVersion() = %q (main=%q/%q)", moduleVersion(), bi.Main.Path, bi.Main.Version)
}

// The test above only proves the helper is honest — it says nothing about
// whether the server actually calls it, and the regression lived at the CALL
// SITE, not in a helper. This one reads the package's own source and fails on
// any `Version:` field initialised from a string literal, wherever it appears.
//
// The ground truth is the source file itself, so a newly added third site is
// covered the moment it is written; there is no list here to forget to update.
//
// Falsification: put `Version: "0.1.0"` back at either site in mcp.go and this
// goes RED naming the file and line.
func TestNoVersionFieldIsAStringLiteral(t *testing.T) {
	// go/parser.ParseDir is deprecated as of Go 1.25 (it ignores build tags), so
	// the directory is walked directly. Every non-test .go file is parsed —
	// build tags do not matter here, because a literal is a literal whichever
	// build it belongs to, and skipping a tagged file would be a hole.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fset := token.NewFileSet()
	var files int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue // a _test.go file legitimately contains version strings
		}
		f, parseErr := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		files++
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Version" {
				return true
			}
			lit, ok := kv.Value.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			t.Errorf("%s: Version is a string literal %s — the version belongs to "+
				"release-please; read it with moduleVersion() instead",
				fset.Position(lit.Pos()), lit.Value)
			return true
		})
	}

	// A guard that parsed nothing passes for the wrong reason.
	if files == 0 {
		t.Fatal("parsed no non-test source files — the guard inspected nothing")
	}
	t.Logf("scanned %d non-test file(s) in package mcp", files)
}
