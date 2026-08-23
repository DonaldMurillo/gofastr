package style

import (
	"sort"
	"strings"
)

// TokenNames returns every CSS custom-property name a theme emits,
// without the leading "--", sorted.
//
// It is derived from ThemeToTokens(DefaultTheme()), not hand-copied, so
// the manifest cannot drift from the emitter: a token added to the theme
// appears here, and a removed one disappears. The "dark."-prefixed keys
// are dropped — the dark palette re-declares the SAME property names
// under a dark-scheme block, so keeping them would double every colour
// entry.
func TokenNames() []string {
	tokens := ThemeToTokens(DefaultTheme())
	out := make([]string, 0, len(tokens))
	for k := range tokens {
		if strings.HasPrefix(k, "dark.") {
			continue
		}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
