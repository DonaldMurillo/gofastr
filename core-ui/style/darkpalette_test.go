package style

import (
	"reflect"
	"strings"
	"testing"
)

// completeDarkMap builds a DarkColors entry for every light color token of
// the default theme, so tests can punch exact holes in it.
func completeDarkMap() map[string]string {
	m := map[string]string{}
	for key := range ThemeToTokens(DefaultTheme()) {
		if strings.HasPrefix(key, "color-") {
			m[strings.TrimPrefix(key, "color-")] = "#0A0A0A"
		}
	}
	return m
}

func TestDarkPaletteGapsLightOnlyNil(t *testing.T) {
	// DefaultTheme is light-only by design; that must never warn.
	if got := DarkPaletteGaps(DefaultTheme()); got != nil {
		t.Errorf("light-only theme: DarkPaletteGaps = %v, want nil", got)
	}
}

func TestDarkPaletteGapsCompleteNil(t *testing.T) {
	th := DefaultTheme()
	th.DarkColors = completeDarkMap()
	if got := DarkPaletteGaps(th); got != nil {
		t.Errorf("complete dark map: DarkPaletteGaps = %v, want nil", got)
	}
}

func TestDarkPaletteGapsSortedMissing(t *testing.T) {
	th := DefaultTheme()
	th.DarkColors = completeDarkMap()
	delete(th.DarkColors, "surface-soft")
	delete(th.DarkColors, "code-border")

	got := DarkPaletteGaps(th)
	want := []string{"code-border", "surface-soft"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DarkPaletteGaps = %v, want %v", got, want)
	}
}

// A key present with an empty value is a gap, and a worse one than a
// missing key. An empty custom property is valid CSS: it overrides the
// light declaration, so `var(--color-surface)` substitutes to nothing and
// the consuming declaration is dropped at computed-value time rather than
// falling back. Counting only missing keys called a broken palette
// complete.
func TestEmptyDarkValueIsAGap(t *testing.T) {
	th := DefaultTheme()
	th.DarkColors = completeDarkMap()
	th.DarkColors["surface"] = ""
	th.DarkColors["text"] = "   "
	got := DarkPaletteGaps(th)
	if len(got) != 2 || got[0] != "surface" || got[1] != "text" {
		t.Errorf("DarkPaletteGaps = %v, want [surface text]", got)
	}
}
