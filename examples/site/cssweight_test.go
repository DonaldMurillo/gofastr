package main

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit/cssweight"
)

// siteCSSCap is the most CSS, gzip-compressed, one site page may ship.
// The heaviest page on 2026-10-26 was /components/icon at 21,215 B after
// adding the searchable catalog's filter toolbar, dropdown, form controls
// and copy tiles. The cap sits a little above it, so further CSS growth
// has to raise it in the same change instead of arriving unnoticed.
const siteCSSCap = 22 << 10

// siteCSSFloor is the least CSS a styled page can ship: the theme alone
// is several times this. A page under it lost its stylesheets, and a
// cap-only gate would pass it.
const siteCSSFloor = 1 << 10

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
		if w.Gzip() < siteCSSFloor {
			t.Errorf("%s ships %s, under the %d B floor: its CSS went missing", p, w, siteCSSFloor)
		}
	}
	t.Logf("heaviest: %s at %d B gzip", at, heaviest)
}
