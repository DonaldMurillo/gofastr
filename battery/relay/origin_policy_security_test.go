package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A relayed response is served on the APP's origin, and some response
// headers set policy for that whole origin, kept by the browser long
// after the response that carried them. NEL with Report-To (or
// Reporting-Endpoints) has the browser report the outcome of requests
// to every URL on the app origin, for max_age seconds, to an endpoint
// the vendor names; report delivery is not governed by the page's CSP.
// Alt-Svc steers later app-origin traffic, Accept-CH and Critical-CH
// make the browser send client hints to every app page (and so, through
// the relay, back to the vendor), Strict-Transport-Security with
// includeSubDomains pins every subdomain of the app, and Set-Login
// changes the origin's FedCM login status. None of it is the vendor's
// to set.
func TestVendorOriginPolicyHeadersStripped(t *testing.T) {
	vendorSet := map[string]string{
		"NEL":                       `{"report_to":"v","max_age":31536000,"success_fraction":1.0}`,
		"Report-To":                 `{"group":"v","max_age":31536000,"endpoints":[{"url":"https://vendor.example/nel"}]}`,
		"Reporting-Endpoints":       `v="https://vendor.example/r"`,
		"Alt-Svc":                   `h2="vendor.example:443"; ma=2592000`,
		"Accept-CH":                 "Sec-CH-UA-Full-Version-List, Sec-CH-UA-Model, Device-Memory",
		"Critical-CH":               "Sec-CH-UA-Model",
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains; preload",
		"Set-Login":                 "logged-in",
	}
	base := startRelay(t, defaultCfg, func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range vendorSet {
			w.Header().Set(k, v)
		}
		w.WriteHeader(http.StatusOK)
	})
	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	for name := range vendorSet {
		if got := res.Header.Values(name); len(got) != 0 {
			t.Errorf("SECURITY: upstream %s reached the browser (%q): it sets policy for the whole app origin", name, got)
		}
	}
}

// The strip is the vendor's copy only: an origin policy the host set
// before the relay ran reaches the browser as the host wrote it.
func TestHostOriginPolicyHeadersKept(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=1; includeSubDomains")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(up.Close)
	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(defaultCfg(up.URL + "/cdn")))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	router := app.Router()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000")
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	res, err := http.Get(srv.URL + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Values("Strict-Transport-Security"); len(got) != 1 || got[0] != "max-age=63072000" {
		t.Errorf("Strict-Transport-Security = %q, want the host's value alone", got)
	}
}
