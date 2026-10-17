package main

import (
	"os"
	"sort"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/testkit/nojstest"
)

// TestNoJSMeridianWorks loads Meridian's marketing, auth, app and admin
// pages with script execution off, and signs in through the native login
// form in the same script-off tab, so the session itself is proof that
// the form posts without the runtime. Run it isolated, like the axe gate:
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

	for _, p := range []string{
		"/", "/pricing", "/about", "/terms", "/privacy", "/login", "/signup",
		"/docs/api", "/docs/api/auth", "/docs/api/errors", "/docs/api/entities/customers",
	} {
		check(p)
	}

	if err := chromedp.Run(tab,
		chromedp.Navigate(base+"/login"),
		chromedp.WaitVisible(`#auth-email`, chromedp.ByQuery),
		chromedp.SendKeys(`#auth-email`, "admin@meridian.dev", chromedp.ByQuery),
		chromedp.SendKeys(`#auth-password`, os.Getenv("ADMIN_SEED_PASSWORD"), chromedp.ByQuery),
		chromedp.Click(`button[type="submit"]`, chromedp.ByQuery),
		chromedp.WaitVisible(`a[href="/app/customers"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("native-form login with script off: %v", err)
	}
	if ps, err := nojstest.Inspect(tab); err != nil {
		t.Errorf("no-JS after login: %v", err)
	} else {
		nojstest.Report(t, "(landing after login)", ps)
	}

	// Record ids and the admin entity list come from script-on tabs on the
	// same browser, which share the session cookie the login just set.
	customerID := axeFirstDetailID(t, browser, base, "/app/customers", "/app/customers")
	invoiceID := axeFirstDetailID(t, browser, base, "/app/invoices", "/app/invoices")
	pages := []string{
		"/app", "/app/customers", "/app/customers/" + customerID,
		"/app/invoices", "/app/invoices/" + invoiceID, "/app/subscriptions",
		"/admin", "/admin/queue", "/admin/audit",
		"/admin/entities/customers/" + axeAdminRowID(t, browser, base, "customers"),
	}
	for _, table := range axeAdminEntityTables(t, browser, base) {
		pages = append(pages, "/admin/entities/"+table, "/admin/entities/"+table+"/create")
	}
	sort.Strings(pages)
	for _, p := range pages {
		check(p)
	}
}
