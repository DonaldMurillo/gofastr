package style

import (
	"strings"
	"testing"
)

func TestNestedKeyframesStayUnscoped(t *testing.T) {
	for _, wrapper := range []string{"media", "container", "both"} {
		t.Run(wrapper, func(t *testing.T) {
			sheet := NewComponentSheet("card", DefaultTheme())
			animation := func(inner *ComponentSheet) {
				inner.Keyframes("spin", Step("from", "opacity", "0"), Step("to", "opacity", "1"))
				inner.Rule(".card").Set("animation", "spin 1s").End()
			}
			switch wrapper {
			case "media":
				sheet.Media("(min-width: 1px)", animation)
			case "container":
				sheet.Container("card", "(min-width: 1px)", animation)
			case "both":
				sheet.Media("(min-width: 1px)", func(inner *ComponentSheet) {
					inner.Container("card", "(min-width: 1px)", animation)
				})
			}
			css, err := sheet.Build()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(css, `[data-cui-comp="card"] from`) || strings.Contains(css, `[data-cui-comp="card"] to`) {
				t.Fatalf("keyframe steps were scoped:\n%s", css)
			}
			if !strings.Contains(css, `[data-cui-comp="card"] .card`) || !strings.Contains(css, "@keyframes spin") {
				t.Fatalf("missing scoped rule or keyframes:\n%s", css)
			}
		})
	}
}
