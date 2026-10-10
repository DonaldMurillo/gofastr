package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The bar and the body are slots: the caller's markup sits in two
// unmarked wrappers, the bar first.
func TestSelectionWrapsBarAndBody(t *testing.T) {
	out := string(Selection(SelectionConfig{ID: "s",
		Bar:  render.HTML(`<form id="bulk"></form>`),
		Body: render.HTML(`<table id="rows"></table>`),
	}))
	for _, want := range []string{
		`<div class="fui-selection" id="s" data-cui-comp="ui-selection">`,
		`<div class="fui-selection__bar"><form id="bulk"></form></div>`,
		`<div class="fui-selection__body"><table id="rows"></table></div>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("selection missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "__bar") > strings.Index(out, "__body") {
		t.Errorf("the bar is not before the body:\n%s", out)
	}
}

func TestSelectionRefusals(t *testing.T) {
	for name, cfg := range map[string]SelectionConfig{
		"no bar":  {Body: render.Text("x")},
		"no body": {Bar: render.Text("x")},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			Selection(cfg)
		})
	}
}

// The hide rule sits behind @supports, so a browser without :has()
// shows the bar always instead of never.
func TestSelectionHidesOnlyWhereHasWorks(t *testing.T) {
	css := selectionStyle.Entry().CSSFor(style.DefaultTheme())
	at := strings.Index(css, "@supports selector(:has(*))")
	hide := strings.Index(css, "display: none")
	if at < 0 || hide < at {
		t.Errorf("the bar's hide rule is not inside @supports selector(:has(*)):\n%s", css)
	}
}

// A floating selection draws the rows first and the bar after them, so
// the bar's count (a CSS counter of the checked rows) has seen every
// row; the bar carries the count's word and, with Form, a reset button
// that clears the rows joined to that form.
func TestSelectionFloating(t *testing.T) {
	out := string(Selection(SelectionConfig{ID: "s", Floating: true, Form: "bulk",
		Bar:  render.HTML(`<form id="bulk"></form>`),
		Body: render.HTML(`<table id="rows"></table>`),
	}))
	if strings.Index(out, "__body") > strings.Index(out, "__bar") {
		t.Errorf("a floating bar is not after the body:\n%s", out)
	}
	for _, want := range []string{
		`fui-selection--floating`,
		`<span class="fui-selection__count" data-cui-internal=""><span data-hui-selection-count="">0</span> selected</span>`,
		`data-hui-selection=""`,
		`<button aria-label="Clear selection" class="fui-selection__clear" data-cui-internal="" form="bulk" type="reset">`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("floating selection missing %q:\n%s", want, out)
		}
	}
	plain := string(Selection(SelectionConfig{Floating: true,
		Bar: render.HTML(`<p>bar</p>`), Body: render.HTML(`<p>body</p>`)}))
	if strings.Contains(plain, `type="reset"`) {
		t.Errorf("a clear button drew with no Form:\n%s", plain)
	}
}

// The select-all box in a table header is not a row: it does not keep
// the bar up.
func TestSelectionIgnoresSelectAll(t *testing.T) {
	css := selectionStyle.Entry().CSSFor(style.DefaultTheme())
	if !strings.Contains(css, `:not([data-hui-table-select-all]):checked`) {
		t.Errorf("the hide rule counts the select-all box:\n%s", css)
	}
}
