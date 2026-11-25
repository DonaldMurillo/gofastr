package ui_test

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// EllipsizeStrings cuts a long string to one line in a narrow box, both
// a member of an object (inside a row) and a root that is itself a
// string (straight in the viewer, with no row around it).
func TestJSONViewerEllipsizesRootAndMemberStrings(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	long := strings.Repeat("a long value ", 20)
	body := `<div id="root" style="width: 160px">` + string(ui.JSONViewer(ui.JSONViewerConfig{Value: long, EllipsizeStrings: true})) + `</div>` +
		`<div id="member" style="width: 160px">` + string(ui.JSONViewer(ui.JSONViewerConfig{Value: map[string]any{"k": long}, EllipsizeStrings: true})) + `</div>`
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(800, 600))
	var got map[string]bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const cut = sel => { const s = document.querySelector(sel + ' .fui-json-viewer__str');
			const cs = getComputedStyle(s);
			return cs.textOverflow === 'ellipsis' && cs.whiteSpace === 'nowrap' && s.scrollWidth > s.clientWidth && s.getClientRects().length === 1; };
		return {root: cut('#root'), member: cut('#member')};
	})()`, &got)); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !got["root"] || !got["member"] {
		t.Errorf("a long string was not cut to one line: %v", got)
	}
}
