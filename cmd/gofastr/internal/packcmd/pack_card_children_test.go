package packcmd

import (
	"go/ast"
	"go/parser"
	"testing"
)

// The generator emits a card with children as ui.Card(cfg, children...);
// pack kept only the heading and text, so the next generate emptied it.
func TestPackKeepsCardChildren(t *testing.T) {
	src := `ui.Card(ui.CardConfig{Heading: "Plans", Description: "Pick one"}, ui.LinkButton(ui.LinkButtonConfig{Label: "Start", Href: "/start"}))`
	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	b, ok := reverseBlock(e.(*ast.CallExpr), nil)
	if !ok || b.Kind != "card" {
		t.Fatalf("reverseBlock = %+v, %v; want a card", b, ok)
	}
	if len(b.Children) != 1 || b.Children[0].Kind != "link_button" {
		t.Fatalf("card children = %+v, want one link_button", b.Children)
	}
	if b.Children[0].Props["href"] != "/start" {
		t.Fatalf("child props = %v", b.Children[0].Props)
	}
}
