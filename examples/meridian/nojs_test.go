package main

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/testkit/nojstest"
)

// TestNoJSMeridianWorks loads the pages the axe gate scans with script
// execution off, and signs in through the native login form in the same
// script-off tab, so the session itself is proof that the form posts
// without the runtime. Run it isolated, like the axe gate:
//
//	go test ./examples/meridian/ -run TestNoJSMeridianWorks
func TestNoJSMeridianWorks(t *testing.T) {
	if testing.Short() {
		t.Skip("no-JS e2e: builds + boots the binary")
	}
	base := e2eBootApp(t)
	browser := axetest.NewBrowser(t)
	tab, cancel := nojstest.NewTab(t, browser)
	defer cancel()

	check := func(p string) {
		t.Helper()
		ps, err := nojstest.Check(tab, base+p)
		if err != nil {
			t.Errorf("no-JS %s: %v", p, err)
			return
		}
		nojstest.Report(t, p, ps)
	}

	for _, p := range meridianAnonPages {
		check(p)
	}

	e2eLogin(t, tab, base)
	if ps, err := nojstest.Inspect(tab); err != nil {
		t.Errorf("no-JS after login: %v", err)
	} else {
		nojstest.Report(t, "(landing after login)", ps)
	}

	// Record ids and the admin entity list come from script-on tabs on the
	// same browser, which share the session cookie the login just set.
	customerID := axeFirstDetailID(t, browser, base, "/app/customers", "/app/customers")
	invoiceID := axeFirstDetailID(t, browser, base, "/app/invoices", "/app/invoices")
	for _, p := range slices.Concat(meridianAppPages(customerID, invoiceID), meridianAdminPages(t, browser, base)) {
		check(p)
	}
}
