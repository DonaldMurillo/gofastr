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
