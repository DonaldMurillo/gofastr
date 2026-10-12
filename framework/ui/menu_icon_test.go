package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func iconMenu(cfg ui.MenuConfig) string {
	cfg.Label = "Columns"
	cfg.Items = []ui.MenuItem{{Label: "Amount", Href: "/x"}}
	return string(ui.Menu(cfg))
}

// Icon draws the glyph and then the label inside the summary trigger.
func TestMenuIconDressesTheTrigger(t *testing.T) {
	out := iconMenu(ui.MenuConfig{ID: "c", Icon: "columns"})
	open := strings.Index(out, `<summary aria-controls="c-panel" aria-haspopup="menu" class="fui-menu__trigger"`)
	end := strings.Index(out, "</summary>")
	if open < 0 || end < open {
		t.Fatalf("no summary trigger:\n%s", out)
	}
	trigger := out[open:end]
	svg := strings.Index(trigger, "<svg")
	label := strings.Index(trigger, `<span data-cui-internal="">Columns</span>`)
	if svg < 0 || label < svg {
		t.Errorf("the trigger is not icon then label:\n%s", trigger)
	}
}

func TestMenuIconRefusals(t *testing.T) {
	for name, cfg := range map[string]ui.MenuConfig{
		"unknown icon":    {Icon: "no-such-icon"},
		"trigger html":    {Icon: "columns", TriggerHTML: render.Text("x")},
		"trigger element": {Icon: "columns", TriggerElement: render.HTML(`<button type="button">x</button>`)},
		"icon only":       {Icon: "columns", IconOnly: true},
		"avatar":          {Icon: "columns", Avatar: &ui.AvatarConfig{Name: "Ada"}},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			iconMenu(cfg)
		})
	}
}
