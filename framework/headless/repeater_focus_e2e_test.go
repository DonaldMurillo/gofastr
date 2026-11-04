package headless

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

func TestRepeaterSkipsHiddenFocus(t *testing.T) {
	renderRows := func(count int) string {
		items := make([]RepeaterItem, count)
		for index := range items {
			items[index].Fields = []render.HTML{
				render.Tag("input", map[string]string{"type": "hidden", "name": "id", "value": "stored"}),
				render.Tag("input", map[string]string{"type": "text", "disabled": ""}),
				render.Tag("input", map[string]string{"type": "text", "data-focus-target": ""}),
			}
		}
		return `<div data-cui-signal="links" data-cui-signal-mode="html">` + string(Repeater(RepeaterProps{
			Name: "links", Label: "Links", Items: items, Action: "/__hui/links",
			Island: Island{Endpoint: "/__hui/links", Signal: "links"},
		}, nil)) + `</div>`
	}
	server := startBehaviorServer(t, renderRows(2), func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/links", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(renderRows(1)))
		})
	})
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) || !pollTrue(ctx, controlsLoadedExpr("rpc")) {
		t.Fatal("repeater runtime did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-repeater-action="remove"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-repeater-item]').length === 1 && document.activeElement.hasAttribute('data-focus-target')`) {
		t.Fatal("removal focused a hidden or disabled field")
	}
}
