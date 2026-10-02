package runtime_test

// The link seam: a registered behaviour is a module from
// runtime.Module down, but only in a binary that links the package
// registering it. This package's own tests walk moduleAttrs (the
// attrdoc gates) and serve /__gofastr/runtime/<name>.js; the rows a
// registered behaviour owns — headless-feedback's toast-stack pair —
// are provable here only when framework/ui (which pulls in
// framework/headless) is linked into the test binary. The menu axe
// and panel suites that used to provide this link moved to
// framework/ui with the Menu migration; this file keeps it, on
// purpose, so the gate keeps proving the row rather than skipping it.

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	_ "github.com/DonaldMurillo/gofastr/framework/ui"
)

// TestRegisteredBehaviourRowsAreServable holds the moduleAttrs keys a
// registered behaviour owns to the contract every embedded module row
// already meets: the name resolves to servable source.
func TestRegisteredBehaviourRowsAreServable(t *testing.T) {
	for _, name := range []string{"headless-feedback"} {
		if _, ok := runtime.Module(name); !ok {
			t.Errorf("%s is a moduleAttrs row owned by a registered behaviour, and it is not servable in this binary — the link seam above broke", name)
		}
	}
}
