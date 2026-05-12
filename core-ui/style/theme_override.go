package style

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// ThemeRef is a handle to a registered theme override. The framework
// emits a `.fui-theme-<hash>` CSS block in app.css that re-declares
// every changed token; wrapping a subtree with this class scopes the
// override to that part of the DOM via the CSS variable cascade.
//
// Apps register an override once at boot:
//
//	var Dark = style.RegisterThemeOverride(darkTheme)
//
// And wrap any subtree:
//
//	ui.Themed(style.Dark, ui.Card{...})
//
// Hash is content-addressed (sha256 of the override's :root output)
// so registering the same theme twice returns the same ref.
type ThemeRef struct {
	Hash string
}

// Class returns the CSS class name applied to wrapped subtrees:
// `fui-theme-<hash>`.
func (r ThemeRef) Class() string { return "fui-theme-" + r.Hash }

var (
	themeOverrideMu sync.Mutex
	themeOverrides  = map[string]Theme{} // hash → theme
)

// RegisterThemeOverride records a theme override and returns its
// handle. Idempotent: same content → same hash → same handle.
//
// The override is registered against the FULL theme (not "diffs vs
// default"). When emitted, the framework walks the theme and emits
// every token under the override class — the browser's cascade
// handles the actual delta vs the canonical :root.
func RegisterThemeOverride(t Theme) ThemeRef {
	css := t.CSSCustomProperties()
	sum := sha256.Sum256([]byte(css))
	hash := hex.EncodeToString(sum[:6])
	themeOverrideMu.Lock()
	defer themeOverrideMu.Unlock()
	if _, ok := themeOverrides[hash]; !ok {
		themeOverrides[hash] = t
	}
	return ThemeRef{Hash: hash}
}

// AllThemeOverrides returns a snapshot of every registered theme,
// keyed by hash. Used by the uihost to emit `.fui-theme-<hash>`
// blocks in app.css.
func AllThemeOverrides() map[string]Theme {
	themeOverrideMu.Lock()
	defer themeOverrideMu.Unlock()
	out := make(map[string]Theme, len(themeOverrides))
	for k, v := range themeOverrides {
		out[k] = v
	}
	return out
}

// ThemeOverrideCSS emits the class-scoped block for one override:
//
//	.fui-theme-<hash> {
//	  --color-primary: …;
//	  --color-text: …;
//	  …
//	}
//
// The class wraps a subtree; descendant components reading
// var(--color-primary) get the overridden value via cascade.
func ThemeOverrideCSS(hash string, t Theme) string {
	var lines []string
	collectTokenDecls(reflect.ValueOf(t), &lines)
	sort.Strings(lines)
	var b strings.Builder
	fmt.Fprintf(&b, ".fui-theme-%s {\n", hash)
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

// AllThemeOverridesCSS emits every registered override as a
// concatenated CSS block. Sorted by hash for byte-stable output.
func AllThemeOverridesCSS() string {
	all := AllThemeOverrides()
	if len(all) == 0 {
		return ""
	}
	hashes := make([]string, 0, len(all))
	for h := range all {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	var b strings.Builder
	for i, h := range hashes {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ThemeOverrideCSS(h, all[h]))
	}
	return b.String()
}
