package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// styleRegistryE2ESentinel proves the package-init contract for
// style.Contribute works end-to-end: a var _ = style.Contribute(...) at
// package scope must fire before setupServer() runs, and the registered
// rules must reach the served /__gofastr/app.css. The uihost fans
// contributions in itself (no style.Apply hand-wiring in styles.go);
// if either link (init firing, uihost's AppCSS fan-in) breaks, the
// assertion below catches it.
var styleRegistryE2ESentinel = style.Contribute(func(ss *style.StyleSheet) {
	ss.Rule(".__test-style-registry-package-init-sentinel").
		Set("color", "{colors.primary}").
		End()
})

func TestStyleRegistry_PackageInitReachesAppCSS(t *testing.T) {
	_ = styleRegistryE2ESentinel // silence unused-var

	base := archStartServer(t)
	css := archFetch(t, base, "/__gofastr/app.css")
	switch n := strings.Count(css, ".__test-style-registry-package-init-sentinel"); n {
	case 1: // exactly once — uihost fan-in, no hand-wiring duplicate
	case 0:
		t.Error("style.Contribute sentinel did NOT reach /__gofastr/app.css — package-init or uihost AppCSS fan-in is broken")
	default:
		t.Errorf("style.Contribute sentinel appears %d times in app.css — contributions are being fanned in twice (uihost AppCSS + a stale style.Apply hand-wiring in styles.go?)", n)
	}
}
