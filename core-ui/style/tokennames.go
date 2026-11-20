package style

import (
	"sort"
	"strings"
)

// TokenNames returns every CSS custom-property name a theme emits, without the
// leading "--", sorted.
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
