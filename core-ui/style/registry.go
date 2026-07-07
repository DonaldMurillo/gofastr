package style

import "sync"

// Co-located scoped styles.
//
// The 3-file roundtrip pain — change a screen, edit the host's theme.go to
// add a CSS rule, reload — is solved by letting screens/components declare
// their CSS next to the Go render code that uses it. Each declaration goes
// through [Contribute]; the framework's uihost calls [Apply] itself while
// assembling /__gofastr/app.css, so a bare Contribute just works — no
// WithCustomCSS hand-wiring. Custom hosts that build their own stylesheet
// call [Apply] once during construction to fan contributions in.
//
// Final CSS is identical to a hand-authored theme.go — no nonces, no
// dev/prod divergence, no inline <style>. The strict CSP stays intact.
//
// Distinct from core-ui/registry.RegisterStyle: that surface registers a
// NAMED, lazy-loaded per-component sheet. Contribute registers a fragment
// that gets fanned into the host's GLOBAL theme stylesheet at boot.
//
// Usage at the package level:
//
//	var _ = style.Contribute(func(ss *style.StyleSheet) {
//	    ss.Rule(".home-hero").
//	        Set("padding", "{spacing.lg}", "background", "{colors.surface}").
//	        End()
//	})
//
// Order: Contribute-time order matches application order in Apply. The
// uihost applies contributions after the app's WithCustomCSS payload, so
// co-located rules can override host base rules by writing the same
// selector.
//
// Trust model: the slice is a global registry. Any imported package can
// add rules at init time, which is the SAME trust model as importing
// Go code at all — a malicious dependency could equally run init() code
// or use stdlib to do worse. Vet dependencies; selectors are not
// sanitised.

var (
	registryMu sync.RWMutex
	registry   []func(*StyleSheet)
)

// Contribute queues fn to be applied to the host's stylesheet during the
// next [Apply] call. The returned struct{} exists purely so callers can
// use the `var _ = ...` idiom at package scope; it carries no state.
//
// Contribute is safe to call from package init() and from package-scope
// variable initialisers. Dynamic registration after Apply is supported,
// but the newly contributed fn only takes effect on the next Apply call.
func Contribute(fn func(*StyleSheet)) struct{} {
	if fn == nil {
		return struct{}{}
	}
	registryMu.Lock()
	registry = append(registry, fn)
	registryMu.Unlock()
	return struct{}{}
}

// Apply runs every [Contribute]'d fn against ss, in registration order.
// The framework's uihost calls this against a fresh sheet each time it
// assembles app.css; custom hosts call it once inside createStyleSheet.
// Multiple calls are supported (e.g. when a host rebuilds the stylesheet
// for theme switching) — each call re-applies the full registry to the
// supplied ss.
func Apply(ss *StyleSheet) {
	if ss == nil {
		return
	}
	registryMu.RLock()
	fns := make([]func(*StyleSheet), len(registry))
	copy(fns, registry)
	registryMu.RUnlock()
	for _, fn := range fns {
		fn(ss)
	}
}

// ResetRegistryForTest empties the registry. Test-only helper — production
// code never needs to call this.
func ResetRegistryForTest() {
	registryMu.Lock()
	registry = nil
	registryMu.Unlock()
}
