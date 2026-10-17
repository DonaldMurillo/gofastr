package main

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit/cssweight"
)

// meridianCSSCap is the most CSS, gzip-compressed, one Meridian page may
// ship. The heaviest page on 2026-10-17 was /admin/audit at 15,425 B; the
// cap sits a little above it, so CSS growth has to raise it in the same
// change instead of arriving unnoticed.
const meridianCSSCap = 17 << 10

// TestCSSWeightMeridian weighs the CSS of the signed-out pages the axe
// gate scans, then of the signed-in app and admin screens, against
// meridianCSSCap.
func TestCSSWeightMeridian(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	base := e2eBootApp(t)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	weigh := func(p string) {
		t.Helper()
		w, err := cssweight.Measure(client, base+p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			return
		}
		t.Logf("%-40s %s", p, w)
		if w.Gzip() > meridianCSSCap {
			t.Errorf("%s ships %d B of gzipped CSS, over the %d B cap", p, w.Gzip(), meridianCSSCap)
		}
	}
	for _, p := range meridianAnonPages {
		weigh(p)
	}
	if _, err := client.PostForm(base+"/auth/login", url.Values{
		"email": {"admin@meridian.dev"}, "password": {os.Getenv("ADMIN_SEED_PASSWORD")},
	}); err != nil {
		t.Fatalf("login: %v", err)
	}
	signedIn := []string{
		"/app", "/app/customers", "/app/invoices", "/app/subscriptions",
		"/app/customers/create", "/admin", "/admin/queue", "/admin/audit",
		"/admin/entities/customers", "/admin/entities/customers/create",
	}
	for _, p := range signedIn {
		weigh(p)
	}
}
