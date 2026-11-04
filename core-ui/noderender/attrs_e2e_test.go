package noderender

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func nodeBrowser(t *testing.T, tree node.Node) context.Context {
	t.Helper()
	body := RenderTrustedNode(tree)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!doctype html><html><body>" + string(body) + "</body></html>"))
	}))
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestFalseAttrsStayFalse(t *testing.T) {
	ctx := nodeBrowser(t, node.Node{Kind: "div", Children: []node.Node{
		{Kind: "input", Props: map[string]any{"id": "field", "type": "checkbox", "disabled": false, "checked": false, "aria-hidden": false}},
		{Kind: "select", Props: map[string]any{"id": "select", "required": false, "multiple": false}},
	}})
	var correct bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const field = document.getElementById('field'), select = document.getElementById('select');
		return !field.disabled && !field.checked && !select.required && !select.multiple && field.getAttribute('aria-hidden') === 'false';
	})()`, &correct)); err != nil {
		t.Fatal(err)
	}
	if !correct {
		t.Fatal("false props enabled HTML boolean attributes")
	}
}
