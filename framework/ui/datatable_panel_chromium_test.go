package ui_test

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// A row menu or dropdown on a table's last row opens in full. The
// table's scroll box clips what hangs past its edge, so a panel left
// inside it was cut off at the table's bottom and opening it scrolled
// the table instead; the panel floats free of the box, and flips above
// its trigger when the viewport has no room below.
func TestDataTablePanelsEscapeTheScrollBox(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := `<meta name="viewport" content="width=device-width, initial-scale=1">` +
		"<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style>"
	menu := ui.Menu(ui.MenuConfig{
		ID: "rowmenu", Label: "Row actions", IconOnly: true, Position: ui.MenuBottomEnd,
		Items: []ui.MenuItem{{Label: "View", Href: "#v"}, {Label: "Copy link", Href: "#c"}, {Label: "Duplicate", Href: "#d"}, {Label: "Delete", Href: "#x"}},
	})
	drop := ui.Dropdown(ui.DropdownConfig{
		ID: "rowdrop", Label: "Edit", Align: ui.DropdownEnd,
		Content: render.HTML(`<p>one</p><p>two</p><p>three</p><p>four</p><p>five</p>`),
	})
	table := ui.DataTable(ui.DataTableConfig{
		Columns: []ui.Column{{Key: "name", Header: "Name"}, {Key: "act", Header: "Actions", Align: "end"}},
		Rows: []ui.Row{
			{Cells: map[string]render.HTML{"name": "First", "act": drop}},
			{Cells: map[string]render.HTML{"name": "Last", "act": menu}},
		},
	})
	// The spacer puts the table's bottom near the viewport's, so the
	// last row's panel has to flip above its trigger.
	for _, tc := range []struct {
		name, id, pad, wrap string
		above               bool
	}{
		{"menu below", "rowmenu", "", "", false},
		{"dropdown below", "rowdrop", "", "", false},
		{"menu flips up", "rowmenu", `<div style="block-size: 640px"></div>`, "", true},
		// A transformed ancestor is the fixed panel's containing block.
		{"inside a transform", "rowmenu", "", "transform: translateX(40px);", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := themeTestPageWithHead(t, head, tc.pad+`<div style="padding: 32px;`+tc.wrap+`">`+string(table)+`</div>`)
			ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(1024, 768), prefersScheme("light"))
			if !pollJS(ctx, moduleLoaded("headless-disclosure")) {
				t.Fatal("headless-disclosure never loaded")
			}
			open := `(() => { const d = (document.getElementById("` + tc.id + `") || document.getElementById("` + tc.id + `-panel")).closest("details"); d.scrollIntoView({block: "nearest"}); d.querySelector(":scope > summary").click(); return true; })()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(open, nil)); err != nil {
				t.Fatal(err)
			}
			settled := `(() => {
				const d = (document.getElementById("` + tc.id + `") || document.getElementById("` + tc.id + `-panel")).closest("details");
				const p = d.querySelector(":scope > :not(summary)");
				const box = d.closest(".fui-data-table__scroll");
				const r = p.getBoundingClientRect(), b = box.getBoundingClientRect(), s = d.querySelector(":scope > summary").getBoundingClientRect();
				if (!d.open || r.height === 0) return "";
				const hit = document.elementFromPoint(r.left + r.width / 2, r.bottom - 4);
				return JSON.stringify({
					shown: !!hit && p.contains(hit),
					boxScrolled: box.scrollTop,
					boxGrew: box.scrollHeight - box.clientHeight,
					above: r.bottom <= s.top + 1,
					below: r.top >= s.bottom - 1,
					inView: r.top >= 0 && r.bottom <= window.innerHeight && r.left >= 0 && r.right <= document.documentElement.clientWidth,
					endAligned: Math.abs(r.right - s.right) <= 1,
					pastBox: r.bottom > b.bottom || r.top < b.top,
				});
			})()`
			var got string
			if !pollJS(ctx, settled+` !== ""`) {
				t.Fatal("the panel never opened")
			}
			// Past the open animation, whose scale shrinks the panel.
			if err := chromedp.Run(ctx, chromedp.Sleep(300*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			got = evalString(ctx, settled)
			for _, want := range []string{`"shown":true`, `"boxScrolled":0`, `"boxGrew":0`, `"inView":true`, `"endAligned":true`} {
				if !strings.Contains(got, want) {
					t.Errorf("want %s, got %s", want, got)
				}
			}
			side := `"below":true`
			if tc.above {
				side = `"above":true`
			}
			if !strings.Contains(got, side) {
				t.Errorf("want %s, got %s", side, got)
			}
			if tc.above {
				// The page scrolls; the panel keeps its place at the trigger.
				gap := `(() => {
					const d = (document.getElementById("` + tc.id + `") || document.getElementById("` + tc.id + `-panel")).closest("details");
					return d.querySelector(":scope > summary").getBoundingClientRect().top - d.querySelector(":scope > :not(summary)").getBoundingClientRect().bottom;
				})()`
				before := evalString(ctx, `String(`+gap+`)`)
				moved := evalString(ctx, `(() => { const y = window.scrollY; window.scrollBy(0, -40); return String(y - window.scrollY); })()`)
				if moved == "0" {
					t.Fatal("the page did not scroll")
				}
				if !pollJS(ctx, `String(`+gap+`) === "`+before+`"`) {
					t.Errorf("the panel left its trigger on scroll: gap %s, was %s", evalString(ctx, `String(`+gap+`)`), before)
				}
			}
			// Closing hands the panel back to the kit's own layout.
			closeJS := `(() => {
				const d = (document.getElementById("` + tc.id + `") || document.getElementById("` + tc.id + `-panel")).closest("details");
				d.querySelector(":scope > summary").click();
				return true;
			})()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(closeJS, nil)); err != nil {
				t.Fatal(err)
			}
			cleared := `(() => {
				const d = (document.getElementById("` + tc.id + `") || document.getElementById("` + tc.id + `-panel")).closest("details");
				return !d.open && d.querySelector(":scope > :not(summary)").style.position === "";
			})()`
			if !pollJS(ctx, cleared) {
				t.Error("the closed panel kept its floating styles")
			}
		})
	}
}
