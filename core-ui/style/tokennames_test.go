package style

import (
	"sort"
	"strings"
	"testing"
)

// The manifest exists so GOFASTR1806 can tell a token reference the theme
// really emits from one it does not (issue #214: `--radius-lg` where the
// theme emits `--radii-lg`, inert for days because an invalid var() is
// not a CSS error). Every property here is load-bearing.

func TestTokenNamesSortedAndComplete(t *testing.T) {
	names := TokenNames()
	if len(names) == 0 {
		t.Fatal("TokenNames is empty — every var() reference would be unknown")
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("TokenNames is not sorted")
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Errorf("duplicate token %q", n)
		}
		seen[n] = true
	}
	if !seen["radii-lg"] {
		t.Errorf("radii-lg missing — the exact token issue #214's typo needed")
	}
	if seen["radius-lg"] {
		t.Errorf("radius-lg present — that is the issue #214 typo, not a token")
	}
	for n := range seen {
		if strings.HasPrefix(n, "dark.") {
			t.Errorf("dark-scheme key %q leaked into the manifest: it re-declares the same property name", n)
		}
	}
}

// The anti-drift assertion: the manifest must be exactly ThemeToTokens
// minus the dark block, so a token added to the theme can never be
// missing from the lint and a removed one can never linger.
func TestTokenNamesMatchThemeToTokens(t *testing.T) {
	want := map[string]bool{}
	for k := range ThemeToTokens(DefaultTheme()) {
		if strings.HasPrefix(k, "dark.") {
			continue
		}
		want[k] = true
	}
	got := TokenNames()
	if len(got) != len(want) {
		t.Errorf("TokenNames has %d entries, ThemeToTokens (dark excluded) has %d", len(got), len(want))
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("TokenNames lists %q, which ThemeToTokens does not emit", n)
		}
	}
}
