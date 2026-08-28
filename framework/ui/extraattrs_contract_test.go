package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// extraAttrsExempt lists Config structs allowed to omit ExtraAttrs.
// Every entry needs a reason; "not done yet" is not one.
var extraAttrsExempt = map[string]string{}

// TestEveryComponentConfigHasExtraAttrs is the completeness gate for the
// ExtraAttrs contract (#251): every exported constructor in this package
// that takes a *Config struct and returns render.HTML or
// component.Component must offer an ExtraAttrs pass-through, so hosts
// can attach data-* test hooks and analytics markers to the element the
// component owns. It parses the package source rather than reflecting,
// because the contract is about declared config surface.
func TestEveryComponentConfigHasExtraAttrs(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	configHasField := map[string]bool{}
	configDeclared := map[string]bool{}
	constructors := map[string][]string{}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.TypeSpec:
				st, ok := d.Type.(*ast.StructType)
				if !ok || !strings.HasSuffix(d.Name.Name, "Config") {
					return true
				}
				configDeclared[d.Name.Name] = true
				for _, fld := range st.Fields.List {
					for _, id := range fld.Names {
						if id.Name == "ExtraAttrs" {
							configHasField[d.Name.Name] = true
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil || !d.Name.IsExported() || d.Type.Results == nil {
					return true
				}
				if !returnsComponentType(d.Type.Results) {
					return true
				}
				for _, p := range d.Type.Params.List {
					typ := p.Type
					if star, ok := typ.(*ast.StarExpr); ok {
						typ = star.X
					}
					if id, ok := typ.(*ast.Ident); ok && strings.HasSuffix(id.Name, "Config") {
						constructors[id.Name] = append(constructors[id.Name], d.Name.Name)
					}
				}
			}
			return true
		})
	}

	if len(constructors) < 40 {
		t.Fatalf("found only %d component configs; the scan is broken", len(constructors))
	}

	for cfg, fns := range constructors {
		if !configDeclared[cfg] {
			continue // declared in another package; its own gate applies
		}
		if reason, ok := extraAttrsExempt[cfg]; ok {
			if configHasField[cfg] {
				t.Errorf("%s is exempt (%q) but has ExtraAttrs; drop the exemption", cfg, reason)
			}
			continue
		}
		if !configHasField[cfg] {
			t.Errorf("%s (used by %s) lacks ExtraAttrs; forward it to the root element or exempt it with a reason",
				cfg, strings.Join(fns, ", "))
		}
	}
}

func returnsComponentType(res *ast.FieldList) bool {
	for _, r := range res.List {
		sel, ok := r.Type.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		if (pkg.Name == "render" && sel.Sel.Name == "HTML") ||
			(pkg.Name == "component" && sel.Sel.Name == "Component") {
			return true
		}
	}
	return false
}
