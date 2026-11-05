package headless

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
)

func TestRepeaterSkipsHiddenFocus(t *testing.T) {
	for _, test := range []struct {
		name     string
		blocked  render.HTML
		fallback bool
	}{
		{name: "hidden identity"},
		{name: "invisible input", blocked: render.Tag("input", map[string]string{"data-focus-hidden": ""})},
		{name: "invisible ancestor", blocked: render.Tag("span", map[string]string{"data-focus-hidden": ""}, render.Tag("input", nil))},
		{name: "collapsed input", blocked: render.Tag("input", map[string]string{"data-focus-collapsed": ""})},
		{name: "add fallback", blocked: render.Tag("input", map[string]string{"data-focus-hidden": ""}), fallback: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			testRepeaterFocus(t, test.blocked, test.fallback, false)
		})
	}
}

func TestRepeaterLastRemovalFocusesPreviousRow(t *testing.T) {
	testRepeaterFocus(t, "", false, true)
}

func testRepeaterFocus(t *testing.T, blocked render.HTML, fallback, removeLast bool) {
	t.Helper()
	initial, remaining := 2, 1
	if removeLast {
		initial, remaining = 3, 2
	}
	renderRows := func(count int) string {
		items := make([]RepeaterItem, count)
		for index := range items {
			items[index].Fields = []render.HTML{
				render.Tag("input", map[string]string{"type": "hidden", "name": "id", "value": "stored"}),
				render.Tag("input", map[string]string{"type": "text", "disabled": ""}),
				blocked,
			}
			if !fallback {
				items[index].Fields = append(items[index].Fields, render.Tag("input", map[string]string{"type": "text", "data-focus-target": strconv.Itoa(index)}))
			}
		}
		return `<div data-cui-signal="links" data-cui-signal-mode="html">` + string(Repeater(RepeaterProps{
			Name: "links", Label: "Links", Items: items, MinItems: 1, Action: "/__hui/links",
			Island: Island{Endpoint: "/__hui/links", Signal: "links"},
		}, nil)) + `</div>`
	}
	server := startBehaviorServer(t, `<style>[data-focus-hidden]{visibility:hidden}[data-focus-collapsed]{visibility:collapse}</style>`+renderRows(initial), func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/links", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(renderRows(remaining)))
		})
	})
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) || !pollTrue(ctx, controlsLoadedExpr("rpc")) {
		t.Fatal("repeater runtime did not load")
	}
	remove := `document.querySelector('[data-hui-repeater-action="remove"]')`
	if removeLast {
		remove = `Array.from(document.querySelectorAll('[data-hui-repeater-action="remove"]')).at(-1)`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(remove+`.click()`, nil)); err != nil {
		t.Fatal(err)
	}
	target := `document.activeElement.hasAttribute('data-focus-target')`
	if fallback {
		target = `document.activeElement.matches('[data-hui-repeater-action="add"]')`
	}
	if removeLast {
		target = `document.activeElement.getAttribute('data-focus-target') === '1'`
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-repeater-item]').length === `+strconv.Itoa(remaining)+` && `+target) {
		t.Fatal("removal did not focus a usable field or the add control")
	}
}
