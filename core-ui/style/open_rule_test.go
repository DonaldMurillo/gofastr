package style

import (
	"strings"
	"testing"
)

func TestOpenRuleSurvivesKeyframes(t *testing.T) {
	sheet := NewStyleSheet(DefaultTheme())
	sheet.Rule(".card").
		Keyframes("first", Step("from", "opacity", "0")).
		Keyframes("second", Step("to", "opacity", "1")).
		Set("color", "red").
		Transition("opacity 1s").
		Pseudo(":hover", "opacity", "0.5").
		Child(".title", "color", "blue").
		Media("(min-width: 1px)", func(inner *StyleSheet) {
			inner.Rule(".wide").Set("display", "block").End()
		}).
		Container("card", "(min-width: 1px)", func(inner *StyleSheet) {
			inner.Rule(".inside").Set("display", "flex").End()
		}).End()
	css := sheet.CSS()
	for _, want := range []string{".card {", "color: red;", "transition: opacity 1s;", ".card:hover", ".card .title", ".wide", ".inside", "@keyframes first", "@keyframes second"} {
		if !strings.Contains(css, want) {
			t.Errorf("open rule lost %q after keyframes:\n%s", want, css)
		}
	}
}
