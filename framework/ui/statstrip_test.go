package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// A strip is one frame around its figures: each cell a slot (the
// caller's markup, a poll region holding a plain StatCard), the count
// of cells on the root for the column rule, a group named by Label.
func TestStatStrip(t *testing.T) {
	out := string(StatStrip(StatStripConfig{Label: "Figures", Cells: []render.HTML{
		StatCard(StatCardConfig{Label: "MRR", Value: "$1", Plain: true}),
		StatCard(StatCardConfig{Label: "Due", Value: "3", Plain: true}),
	}}))
	for _, want := range []string{
		`class="fui-stat-strip fui-stat-strip--2"`,
		`role="group"`, `aria-label="Figures"`,
		`<div class="fui-stat-strip__cell" data-cui-internal=""><div class="fui-stat-card fui-stat-card--plain"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("strip missing %q:\n%s", want, out)
		}
	}
	for name, cells := range map[string][]render.HTML{"none": nil, "seven": make([]render.HTML, 7)} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			StatStrip(StatStripConfig{Cells: cells})
		})
	}
}

// A plain card draws no frame of its own: the strip's frame is its.
func TestStatCardPlainDropsItsFrame(t *testing.T) {
	css := statCardStyle.Entry().CSSFor(style.DefaultTheme())
	at := strings.Index(css, `.fui-stat-card--plain`)
	if at < 0 || !strings.Contains(css[at:], "border: 0") || !strings.Contains(css[at:], "box-shadow: none") {
		t.Errorf("no plain variant that drops the card's frame:\n%s", css)
	}
	if strings.Contains(string(StatCard(StatCardConfig{Label: "a", Value: "1"})), "--plain") {
		t.Error("a card is plain without asking")
	}
}

// A tile card marks its root; the sheet lays the head out in two lines.
func TestStatCardTile(t *testing.T) {
	if !strings.Contains(string(StatCard(StatCardConfig{Label: "a", Value: "1", Icon: "users", Tile: true})), "fui-stat-card--tile") {
		t.Error("no tile modifier")
	}
	css := statCardStyle.Entry().CSSFor(style.DefaultTheme())
	if !strings.Contains(css, `grid-template-areas: "icon . action" "label label label"`) {
		t.Errorf("the tile head is not two lines:\n%s", css)
	}
}
