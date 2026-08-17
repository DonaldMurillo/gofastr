package widget

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestDefaultSkeletonEscapesApostrophes pins the canonical 5-char
// escaper on the widget chrome attributes (name, role, aria-labelledby,
// aria-describedby, slot names). The chrome used to carry a private
// 4-char attribute escaper (no ') — the reduced shape kiln/chat
// documents as a real attribute-breakout XSS class.
func TestDefaultSkeletonEscapesApostrophes(t *testing.T) {
	def := Definition{
		Name:        `w'idget`,
		Position:    BottomRight,
		Role:        `ro'le`,
		LabelledBy:  `lb'l`,
		DescribedBy: `db'y`,
	}
	out := string(defaultSkeleton(def, map[string]render.HTML{`bo'dy`: render.Text("x")}))
	for _, want := range []string{
		`data-fui-widget="w&#39;idget"`,
		`role="ro&#39;le"`,
		`aria-labelledby="lb&#39;l"`,
		`aria-describedby="db&#39;y"`,
		`fui-slot-bo&#39;dy`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("widget chrome missing escaped %q:\n%s", want, out)
		}
	}
	for _, raw := range []string{`w'idget`, `ro'le`, `lb'l`, `db'y`, `bo'dy`} {
		if strings.Contains(out, raw) {
			t.Errorf("widget chrome leaked raw %q:\n%s", raw, out)
		}
	}
}
