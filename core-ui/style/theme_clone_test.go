package style

import (
	"strings"
	"testing"
)

type cloneGlowTokens struct{ Glow Color }

// Registration copies Extensions like the maps: a hand-built slice of
// token-set pointers written after RegisterThemeOverride must not reach
// the hash or the CSS served under it.
func TestOverrideCopiesExtensions(t *testing.T) {
	glow := &cloneGlowTokens{Glow: Color{Name: "clone-glow", Value: "#123456"}}
	th := DefaultTheme()
	th.Extensions = []any{glow}
	ref := RegisterThemeOverride(th)
	glow.Glow.Value = "#ABCDEF" // after registration, before the first hash

	got, ok := AllThemeOverrides()[ref.Hash()]
	if !ok {
		t.Fatal("the registered override is missing from the snapshot")
	}
	css := ThemeOverrideCSS(ref.Hash(), got)
	if !strings.Contains(css, "--color-clone-glow") {
		t.Fatalf("precondition: the extension token must reach the CSS:\n%s", css)
	}
	if !strings.Contains(css, "#123456") || strings.Contains(css, "#ABCDEF") {
		t.Errorf("a write after registration reached the override:\n%s", css)
	}
}

// AllThemeOverrides hands out copies: writing a returned theme's
// extension changes nothing the next snapshot serves.
func TestOverrideSnapshotCopiesExtensions(t *testing.T) {
	th := DefaultTheme()
	th.Extensions = []any{&cloneGlowTokens{Glow: Color{Name: "snap-glow", Value: "#224466"}}}
	ref := RegisterThemeOverride(th)

	snap := AllThemeOverrides()[ref.Hash()]
	snap.Extensions[0].(*cloneGlowTokens).Glow.Value = "#FEDCBA"

	css := ThemeOverrideCSS(ref.Hash(), AllThemeOverrides()[ref.Hash()])
	if !strings.Contains(css, "#224466") || strings.Contains(css, "#FEDCBA") {
		t.Errorf("a write to a snapshot reached the registered override:\n%s", css)
	}
}

// Clone shares no memory with its source, and keeps an entry it has
// nothing to copy in as it is: a nil or non-struct extension does not
// panic.
func TestThemeCloneIsDeep(t *testing.T) {
	glow := &cloneGlowTokens{Glow: Color{Name: "g", Value: "#111111"}}
	th := DefaultTheme()
	th.DarkColors = map[string]string{"primary": "#222222"}
	th.Knobs = map[string]string{"radius": "4px"}
	th.Extensions = []any{glow, nil, 7}

	c := th.Clone()
	c.DarkColors["primary"] = "#333333"
	c.Knobs["radius"] = "8px"
	c.Extensions[0].(*cloneGlowTokens).Glow.Value = "#444444"
	c.Extensions[1] = "x"

	if th.DarkColors["primary"] != "#222222" || th.Knobs["radius"] != "4px" {
		t.Error("a write to the clone's maps reached the source")
	}
	if glow.Glow.Value != "#111111" {
		t.Error("a write to the clone's extension reached the source")
	}
	if th.Extensions[1] != nil || c.Extensions[2] != 7 {
		t.Errorf("entries with nothing to copy changed: %v / %v", th.Extensions, c.Extensions)
	}
}
