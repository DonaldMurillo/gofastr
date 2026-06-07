package runtime

import (
	"os"
	"strings"
	"testing"
)

// TestSignalModulesTearDownOnNavigate guards against the signal-listener leak
// in the animate + computed runtime modules. Both subscribe element closures
// into G._signals[name].listeners on wire; before the fix neither removed them
// on SPA navigation, so every page swap appended listeners (and the detached
// DOM nodes they close over) that were never reclaimed.
//
// We cannot drive a real DOM + gofastr:navigate event from Go without chromedp,
// so this asserts at the source level (same style as TestComputedModule_NoEval)
// that each module both registers a gofastr:navigate handler AND splices its
// closures out of a signal slot's listeners array. That is the load-bearing
// shape of the teardown; a regression that drops either half fails here.
func TestSignalModulesTearDownOnNavigate(t *testing.T) {
	for _, name := range []string{"animate", "computed"} {
		raw, err := os.ReadFile("src/" + name + ".js")
		if err != nil {
			t.Fatalf("read src/%s.js: %v", name, err)
		}
		src := string(raw)

		if !strings.Contains(src, "gofastr:navigate") {
			t.Errorf("src/%s.js: no gofastr:navigate handler — per-page signal "+
				"listeners leak across SPA swaps", name)
		}
		// The teardown must actually remove listeners from a signal slot, not
		// merely re-scan. splice on a listeners array is the only mechanism that
		// reclaims a stale subscription.
		if !strings.Contains(src, ".listeners.splice(") {
			t.Errorf("src/%s.js: does not splice stale closures out of "+
				"G._signals[...].listeners — the leak is not torn down", name)
		}
		// And it must only remove DETACHED elements (isConnected gate), otherwise
		// it would tear down still-live subscriptions.
		if !strings.Contains(src, "isConnected") {
			t.Errorf("src/%s.js: teardown lacks an isConnected gate — it must only "+
				"reclaim detached elements", name)
		}
	}
}
