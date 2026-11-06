package noderender

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
	"github.com/chromedp/chromedp"
)

func TestOwnedAttrsIgnoreCaseVariants(t *testing.T) {
	ctx := nodeBrowser(t, node.Node{Kind: "div", Props: map[string]any{
		"id": "right", "ID": "wrong", "class": "chosen", "Class": "discarded",
	}})
	var correct bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.firstElementChild.id === 'right' && document.body.firstElementChild.className === 'chosen'`, &correct)); err != nil {
		t.Fatal(err)
	}
	if !correct {
		t.Fatal("case-variant extras overrode owned attributes")
	}
}
