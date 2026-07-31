package uihost

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// TestReleaseThemeVariantEvictsComponentCSSCaches pins that releasing the last
// holder of a theme variant evicts that theme's entries from every registry
// component's CSS cache. The per-component cssCache/versionCache (keyed by theme
// hash) grow on first render and were never cleared, so cycling ?theme= values
// — or an embed rebranding per request — grew RAM permanently. Variant release
// is the only point that knows a theme is truly gone, so eviction hooks there.
func TestReleaseThemeVariantEvictsComponentCSSCaches(t *testing.T) {
	ds := hostWithTheme(t, style.DefaultTheme())
	th := brandTheme("#0D9488")
	key := ds.RegisterThemeVariant(th)

	// A registered component whose CSS we warm under the variant theme.
	st := registry.RegisterStyle("uihost-evict-test", func(style.Theme) string {
		return ".uihost-evict-test{color:red}"
	})
	entry := st.Entry()
	themeHash := style.ThemeHash(th)

	entry.CSSFor(th) // warm the cache for the variant theme
	if !entry.HasCachedTheme(themeHash) {
		t.Fatal("precondition: variant theme should be cached after CSSFor")
	}

	ds.ReleaseThemeVariant(key)

	if entry.HasCachedTheme(themeHash) {
		t.Fatal("registry component CSS cache for the released variant theme was not evicted — cycling ?theme= grows RAM permanently")
	}
}

// The app's OWN theme cache is never touched by a variant release: a host that
// registers a variant equal to its active theme, then releases it, must not
// evict the hot-path cache every other request still depends on.
func TestReleaseThemeVariantKeepsAppThemeCache(t *testing.T) {
	appTheme := brandTheme("#4F46E5")
	ds := hostWithTheme(t, appTheme)

	st := registry.RegisterStyle("uihost-evict-keeps", func(style.Theme) string {
		return ".uihost-evict-keeps{color:blue}"
	})
	entry := st.Entry()
	appHash := style.ThemeHash(appTheme)
	entry.CSSFor(appTheme)
	if !entry.HasCachedTheme(appHash) {
		t.Fatal("precondition: app theme should be cached")
	}

	// Register then release a DISTINCT variant — the app theme cache survives.
	key := ds.RegisterThemeVariant(brandTheme("#0D9488"))
	ds.ReleaseThemeVariant(key)

	if !entry.HasCachedTheme(appHash) {
		t.Fatal("releasing an unrelated variant evicted the app's own theme cache — the hot path would rebuild every request")
	}
}
