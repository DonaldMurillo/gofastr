package app

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestIDNamespaceIsOneToOne(t *testing.T) {
	addrs := []string{
		"l:shell#aside", "l:shell#aside-a", "l:shell#aside_a", "l:shell#Aside",
		"l:a-b#c", "l:a#b-c", "g:/g:l:a#b", "l:shell#aside-",
	}
	seen := map[string]string{}
	for _, a := range addrs {
		ns := idNamespace(a)
		if prev, ok := seen[ns]; ok {
			t.Errorf("%q and %q share namespace %q", prev, a, ns)
		}
		seen[ns] = a
	}
	if got := idNamespace("l:shell#aside"); got != "shell-aside" {
		t.Errorf("l:shell#aside = %q, want shell-aside", got)
	}
}

// An island named like an outlet's namespace, or like its address,
// never derives the outlet's ids.
func TestOutletAndIslandNamespacesDiffer(t *testing.T) {
	page := render.WithIDScope(context.Background())
	for _, addr := range []string{"l:shell#aside", "l:shell#aside-a"} {
		outlet := render.UniqueID(outletIDs(page, addr), "features")
		for _, island := range []string{addr, idNamespace(addr), "l-shell-aside", "l_3ashell_23aside-a"} {
			if got := render.UniqueID(render.WithIDNamespace(render.WithIDScope(context.Background()), island), "features"); got == outlet {
				t.Errorf("island %q and outlet %q both derive %q", island, addr, got)
			}
		}
	}
}
