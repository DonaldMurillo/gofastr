package style

import (
	"sort"
	"strings"
)

// DarkPaletteGaps returns the color tokens a theme declares in Colors but
// has no usable dark value for, sorted by token name. A key present with
// an EMPTY value counts as a gap: darkSchemeCSS writes it out as
// `--color-surface: ;`, a malformed declaration the browser drops, so the
// token falls back to its light value exactly as a missing key would.
//
// An empty DarkColors is a supported, deliberate configuration (a
// light-only theme, see Theme.DarkColors) and returns nil. A non-empty
// DarkColors with holes is the case that bites (#215): under a dark
// preference the omitted tokens silently keep their light values, usually
// a contrast bug, and nothing in the render path says so.
//
// Token names come from ThemeToTokens filtered to the "color-" prefix
// rather than a hand-copied field list: the map is built by the same
// reflection walk the CSS emitter uses (walkTokens), so a token added to
// ColorSet shows up here without a second list to keep in sync. Light
// colors key as "color-<name>" and dark entries as "dark.color-<name>",
// so the prefix selects exactly the light palette names DarkColors is
// keyed by.
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
