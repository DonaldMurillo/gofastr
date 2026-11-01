package appgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// uiConfigsWithCtx parses framework/ui and returns the config struct
// types that carry a Ctx field: the components that say their own words
// through the request context.
func uiConfigsWithCtx(t *testing.T) map[string]bool {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "framework", "ui")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fl := range st.Fields.List {
				for _, name := range fl.Names {
					if name.Name == "Ctx" {
						out[ts.Name.Name] = true
					}
				}
			}
			return true
		})
	}
	if !out["PricingCardConfig"] {
		t.Fatal("found no Ctx-bearing ui configs: the parse is vacuous")
	}
	return out
}

// TestEmittedUIConfigsSetCtx: the ctxless analyzer cannot see emitted
// code, so this is its twin for the generator. Every ui config literal
// the union app emits whose type has a Ctx field must set it, or the
// component says its words (a featured plan's "Recommended" badge, a
// table's sort labels) in English on every locale.
//
//gofastr:test-reads framework/ui/
func TestEmittedUIConfigsSetCtx(t *testing.T) {
	prev := FontFetcher
	FontFetcher = func(string) ([]byte, error) { return nil, fmt.Errorf("offline") }
	t.Cleanup(func() { FontFetcher = prev })

	withCtx := uiConfigsWithCtx(t)
	bp, err := appspec.LoadAppSpec(filepath.Join("testdata", "union", "gofastr.yml"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, file := range files {
		// Emitted tests render fixtures with no request in scope, the
		// posture the ctxless analyzer keeps too.
		if !strings.HasSuffix(file.Name, ".go") || strings.HasSuffix(file.Name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file.Name, file.Content, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			sel, ok := lit.Type.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "ui" || !withCtx[sel.Sel.Name] {
				return true
			}
			seen[sel.Sel.Name]++
			for _, el := range lit.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Ctx" {
						return true
					}
				}
			}
			t.Errorf("%s: ui.%s literal leaves Ctx unset", fset.Position(lit.Pos()), sel.Sel.Name)
			return true
		})
	}
	// The union spec must keep exercising the arms this pins.
	for _, want := range []string{"PricingCardConfig", "DataTableConfig", "ButtonConfig", "FormConfig", "TextAreaConfig", "SidebarConfig", "ContentRowConfig"} {
		if seen[want] == 0 {
			t.Errorf("the union app emits no ui.%s literal: the check above is vacuous for it", want)
		}
	}
}
