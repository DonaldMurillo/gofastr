package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestBackToTopTargetDocs(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "backtotop.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var comment string
	ast.Inspect(source, func(node ast.Node) bool {
		field, ok := node.(*ast.Field)
		if ok && len(field.Names) == 1 && field.Names[0].Name == "ScrollTarget" {
			comment = field.Doc.Text()
		}
		return true
	})
	if !strings.Contains(comment, "element id") || strings.Contains(comment, "Set to a CSS selector") {
		t.Fatalf("ScrollTarget must document an element id, not a selector: %s", comment)
	}
	for _, target := range []string{"main-content", "#main-content", "article:résumé"} {
		body := string(BackToTop(BackToTopConfig{ScrollTarget: target}))
		if !strings.Contains(body, `data-hui-back-to-top-target="`+strings.TrimPrefix(target, "#")+`"`) {
			t.Errorf("target %q missing from rendered control: %s", target, body)
		}
	}
}
