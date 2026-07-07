package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The two inventory surfaces this package promises to keep complete:
// the package-doc component list in doc.go, and the one-page catalog at
// framework/docs/content/ui-new-components.md (which claims to index
// "every UI component"). Both tests AST-walk this directory for exported
// Config-taking constructors — mirroring
// examples/site/components_coverage_test.go — and fail on any absentee,
// so a new component can't land without its inventory lines.

// docCatalogExempt lists constructors deliberately absent from
// ui-new-components.md, each with the reason. doc.go has no exemptions —
// the package doc lists everything.
var docCatalogExempt = map[string]string{
	"AspectRatioComponent": "exported alias; documented as AspectRatio (slug aspectratio)",
	"SidebarBody":          "internal building block of Sidebar; documented via the sidebar entry",
	"SkeletonAvatar":       "preset of core-ui/patterns/skeleton; documented via the skeleton entry",
	"SkeletonCard":         "preset of core-ui/patterns/skeleton; documented via the skeleton entry",
	"SkeletonRow":          "preset of core-ui/patterns/skeleton; documented via the skeleton entry",
}

func TestDocGoInventoryComplete(t *testing.T) {
	src, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}
	checkInventory(t, string(src), nil,
		"doc.go package-doc inventory")
}

func TestCatalogDocListsAllComponents(t *testing.T) {
	path := filepath.Join("..", "docs", "content", "ui-new-components.md")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	checkInventory(t, string(src), docCatalogExempt,
		"framework/docs/content/ui-new-components.md")
}

// checkInventory fails the test for every exported Config-taking
// constructor in this package whose name does not appear (as a whole
// word) in doc and is not exempt.
func checkInventory(t *testing.T, doc string, exempt map[string]string, where string) {
	t.Helper()
	var missing []string
	for _, name := range componentConstructorNames(t) {
		if _, ok := exempt[name]; ok {
			continue
		}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if !re.MatchString(doc) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("components missing from %s: %v\n"+
			"Add a one-liner for each (or an exemption with a reason).",
			where, missing)
	}
}

// componentConstructorNames AST-walks the package directory and returns
// every exported func whose first parameter type ends in "Config" —
// the package's component-constructor idiom. Helpers that take no
// Config (Muted, Themed, HighlightLines, …) are out of scope.
func componentConstructorNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	var names []string
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".go") || strings.HasSuffix(de.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, de.Name(), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", de.Name(), err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if fn.Type.Params == nil || len(fn.Type.Params.List) == 0 {
				continue
			}
			ident, ok := fn.Type.Params.List[0].Type.(*ast.Ident)
			if !ok || !strings.HasSuffix(ident.Name, "Config") {
				continue
			}
			names = append(names, fn.Name.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("no component constructors found — AST walk broken?")
	}
	return names
}
