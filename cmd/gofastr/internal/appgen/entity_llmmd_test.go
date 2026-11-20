package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

func TestGeneratedEntityLLMMDConfig(t *testing.T) {
	for _, value := range []string{"", "false", "true"} {
		t.Run("value="+value, func(t *testing.T) {
			src := "app:\n  name: Demo\n  module: example.com/demo\n"
			if value != "" {
				src += "  entity_llm_md: " + value + "\n"
			}
			node, err := coreyaml.Parse(src)
			if err != nil {
				t.Fatal(err)
			}
			spec, err := appspec.DecodeAppSpec(node)
			if err != nil {
				t.Fatal(err)
			}
			file, err := parser.ParseFile(token.NewFileSet(), "main.go", RenderAppSpecMain(spec), 0)
			if err != nil {
				t.Fatal(err)
			}
			configs := 0
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				typ, ok := lit.Type.(*ast.SelectorExpr)
				if !ok || typ.Sel.Name != "AppConfig" {
					return true
				}
				configs++
				found := false
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "NoLLMMD" {
						continue
					}
					found = true
					got, ok := kv.Value.(*ast.Ident)
					want := "false"
					if value == "false" {
						want = "true"
					}
					if !ok || got.Name != want {
						t.Errorf("NoLLMMD = %v, want %s", kv.Value, want)
					}
				}
				if found != (value != "") {
					t.Errorf("NoLLMMD present = %v for setting %q", found, value)
				}
				return true
			})
			if configs != 1 {
				t.Fatalf("generated %d AppConfig literals, want 1", configs)
			}
		})
	}
}
