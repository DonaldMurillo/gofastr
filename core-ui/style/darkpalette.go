package style

import (
	"sort"
	"strings"
)

// DarkPaletteGaps returns the color tokens a theme declares in Colors but has
// no usable dark value for, sorted by token name.
func DarkPaletteGaps(t Theme) []string {
	if len(t.DarkColors) == 0 {
		return nil
	}
	var gaps []string
	for key := range ThemeToTokens(t) {
		if !strings.HasPrefix(key, "color-") {
			continue
		}
		name := strings.TrimPrefix(key, "color-")
		if strings.TrimSpace(t.DarkColors[name]) == "" {
			gaps = append(gaps, name)
		}
	}
	sort.Strings(gaps)
	return gaps
}
