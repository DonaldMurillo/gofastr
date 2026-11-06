package noderender

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
	"github.com/chromedp/chromedp"
)

func TestLinkKeepsTextAndChildren(t *testing.T) {
	ctx := nodeBrowser(t, node.Node{Kind: "link", Props: map[string]any{"href": "/target", "text": "Open "},
		Children: []node.Node{{Kind: "strong", Props: map[string]any{"text": "target"}}},
	})
	var correct bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('a').textContent === 'Open target' && !!document.querySelector('a strong')`, &correct)); err != nil {
		t.Fatal(err)
	}
	if !correct {
		t.Fatal("link dropped its child markup")
	}
}
