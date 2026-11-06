package noderender

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
	"github.com/chromedp/chromedp"
)

func TestMissingPropsDegrade(t *testing.T) {
	for _, tree := range []node.Node{
		{Kind: "label", Props: map[string]any{"text": "Email"}},
		{Kind: "fieldset"},
		{Kind: "img", Props: map[string]any{"alt": "chart"}},
		{Kind: "link", Props: map[string]any{"text": "Link"}},
	} {
		t.Run(tree.Kind, func(t *testing.T) {
			defer func() {
				if problem := recover(); problem != nil {
					t.Fatalf("missing prop panicked: %v", problem)
				}
			}()
			ctx := nodeBrowser(t, tree)
			var safe bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`!document.querySelector('img:not([src]),img[src=""],a:not([href]),a[href=""]')`, &safe)); err != nil {
				t.Fatal(err)
			}
			if !safe {
				t.Fatal("invalid media or link remained interactive")
			}
		})
	}
}
