package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// styleRegistryE2ESentinel proves the package-init contract for
// style.Register works end-to-end: a var _ = style.Contribute(...) at
// package scope must fire before setupServer() runs, and the registered
// rules must reach the served /__gofastr/app.css. If either link
// (init firing, theme.go calling Materialize, app.css concat) breaks,
// the assertion below catches it.
var styleRegistryE2ESentinel = style.Contribute(func(ss *style.StyleSheet) {
	ss.Rule(".__test-style-registry-package-init-sentinel").
		Set("color", "{colors.primary}").
		End()
})

func TestStyleRegistry_PackageInitReachesAppCSS(t *testing.T) {
	_ = styleRegistryE2ESentinel // silence unused-var

	base := archStartServer(t)
	css := archFetch(t, base, "/__gofastr/app.css")
	if !strings.Contains(css, ".__test-style-registry-package-init-sentinel") {
		t.Error("style.Register sentinel did NOT reach /__gofastr/app.css — package-init or Materialize wiring is broken")
	}
}
