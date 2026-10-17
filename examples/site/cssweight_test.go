package main

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit/cssweight"
)

// siteCSSCap is the most CSS, gzip-compressed, one site page may ship.
// The heaviest of the 298 pages on 2026-10-17 was /components/rpc-navigate
// at 15,663 B; the cap sits a little above it, so CSS growth has to raise
// it in the same change instead of arriving unnoticed.
const siteCSSCap = 17 << 10

// TestCSSWeightSite weighs the CSS of every page the axe gate scans
// against siteCSSCap.
func TestCSSWeightSite(t *testing.T) {
	if testing.Short() {
		t.Skip("boots the site")
	}
	base := startE2EServer(t)
	heaviest, at := 0, ""
	for _, p := range axePages(t) {
		w, err := cssweight.Measure(http.DefaultClient, base+p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if w.Gzip() > heaviest {
			heaviest, at = w.Gzip(), p
		}
		if w.Gzip() > siteCSSCap {
			t.Errorf("%s ships %s, over the %d B cap", p, w, siteCSSCap)
		}
	}
	t.Logf("heaviest: %s at %d B gzip", at, heaviest)
}
