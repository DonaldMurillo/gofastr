package appgen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestHiddenRelationSurvivesGeneration(t *testing.T) {
	files, err := RenderGeneratedProject([]framework.EntityDeclaration{{
		Name:      "users",
		Fields:    []framework.FieldDeclaration{{Name: "name", Type: "string"}},
		Relations: []framework.Relation{{Type: framework.RelHasMany, Name: "notes", Entity: "notes", ForeignKey: []string{"user_id"}, Hidden: true}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	for _, file := range files {
		if file.Name != "users.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name, file.Content, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Entity" {
				return true
			}
			decl, err := appspec.PackEntityDeclFromCall(call)
			if err != nil {
				t.Fatal(err)
			}
			checked = true
			if len(decl.Relations) != 1 || !decl.Relations[0].Hidden {
				t.Fatalf("generated and packed relation lost Hidden: %+v", decl.Relations)
			}
			return false
		})
	}
	if !checked {
		t.Fatal("generated users entity was not inspected")
	}
}
