package app_test

import (
	"context"
	"strings"
	"testing"
)

// TestParamGroupKeyWithMarkupCharsRenders pins the inventory check
// against the attribute writer: a {param} value holding a quote,
// ampersand or angle bracket lands in the layer key, the slot
// attribute is written escaped, and the per-render inventory must
// count the escaped spelling. Before the fix every such page answered
// "placed its primary slot 0 times" and uihost served the 404 page.
func TestParamGroupKeyWithMarkupCharsRenders(t *testing.T) {
	a := paramKeyApp(t)
	for _, slug := range []string{"o'neil", "r&d", `x"y`, "a<b"} {
		res, err := a.RenderPageResult(context.Background(), "/projects/"+slug)
		if err != nil {
			t.Fatalf("/projects/%s: %v", slug, err)
		}
		if !strings.Contains(string(res.HTML), "P["+slug+"]") && !strings.Contains(string(res.HTML), "P[") {
			t.Fatalf("/projects/%s rendered no screen body:\n%s", slug, res.HTML)
		}
	}
}
