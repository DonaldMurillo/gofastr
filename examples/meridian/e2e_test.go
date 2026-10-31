// Seeded by `gofastr generate app gofastr.yml`, then hand-evolved.
// Meridian is hand-maintained and NOT regenerable. See doc.go.

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	// The seeded admin's password lives in the generated .env, not in
	// committed source. Load it before the server child inherits the
	// environment (it needs ADMIN_SEED_PASSWORD to seed the account).
	_ = dotenv.LoadAndApply(framework.DefaultDotEnvPaths()...)
	adminPass := os.Getenv("ADMIN_SEED_PASSWORD")
	if adminPass == "" {
		// Fresh checkout: the gitignored .env is absent. The child seeds
		// the admin from whatever ADMIN_SEED_PASSWORD it inherits, so a
		// test-local value keeps the suite self-contained.
		adminPass = "e2e-seed-admin-pw"
		t.Setenv("ADMIN_SEED_PASSWORD", adminPass)
	}
	base := e2eBootApp(t)

	if code, body := e2eDo(t, http.DefaultClient, "GET", base+"/", ""); code != http.StatusOK || !strings.Contains(body, "Meridian") {
		t.Errorf("home page = %d, missing brand? %v", code, !strings.Contains(body, "Meridian"))
	}

	// Public screens render for anonymous visitors.
	for _, p := range []string{"/", "/pricing", "/about", "/terms", "/privacy", "/login", "/signup"} {
		if code, body := e2eDo(t, http.DefaultClient, "GET", base+p, ""); code != http.StatusOK {
			t.Errorf("public screen %s = %d, want 200", p, code)
		} else if len(body) < 120 {
			t.Errorf("public screen %s body suspiciously short (%d bytes)", p, len(body))
		}
	}

	// Gated screens redirect anonymous callers to the login page.
	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, p := range []string{"/app", "/app/customers", "/app/invoices", "/app/subscriptions", "/app/customers/create", "/app/invoices/create", "/app/subscriptions/create"} {
		if r, err := noRedir.Get(base + p); err == nil {
			r.Body.Close()
			if r.StatusCode != http.StatusSeeOther {
				t.Errorf("gated %s anon = %d, want 303 redirect", p, r.StatusCode)
			}
		}
	}

	// Locale prefixes: /es/ serves the Spanish catalog with its links,
	// alternates, sitemap and robots rules; /en/ folds onto the plain path.
	if code, body := e2eDo(t, http.DefaultClient, "GET", base+"/es/pricing", ""); code != http.StatusOK {
		t.Errorf("/es/pricing = %d, want 200", code)
	} else {
		// The footer's language switcher links this page in English
		// and names itself in Spanish.
		for _, want := range []string{"Contactar con ventas", `href="/es/about"`, `hreflang="es"`, `hreflang="x-default"`,
			`href="/pricing" hreflang="en" lang="en" rel="alternate"`, ">Idioma<", ">Español<"} {
			if !strings.Contains(body, want) {
				t.Errorf("/es/pricing lacks %s", want)
			}
		}
	}
	if r, err := noRedir.Get(base + "/en/pricing"); err == nil {
		r.Body.Close()
		if r.StatusCode != http.StatusPermanentRedirect || r.Header.Get("Location") != "/pricing" {
			t.Errorf("/en/pricing = %d Location %q, want 308 to /pricing", r.StatusCode, r.Header.Get("Location"))
		}
	}
	if r, err := noRedir.Get(base + "/es/app"); err == nil {
		r.Body.Close()
		if loc := r.Header.Get("Location"); r.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/es/login") {
			t.Errorf("anon /es/app = %d Location %q, want 303 to /es/login", r.StatusCode, loc)
		}
	}
	if _, body := e2eDo(t, http.DefaultClient, "GET", base+"/sitemap.xml", ""); !strings.Contains(body, "/es/pricing</loc>") {
		t.Errorf("sitemap lacks /es/pricing:\n%s", body)
	}
	if _, body := e2eDo(t, http.DefaultClient, "GET", base+"/robots.txt", ""); !strings.Contains(body, "Disallow: /es/app\n") {
		t.Errorf("robots.txt does not keep crawlers out of /es/app:\n%s", body)
	}

	// Sign in, then every gated screen renders. A redirect to a login
	// page is returned, not followed: with no session the login page's
	// 200 passed for each screen (2026-10-27 G14).
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if req.URL.Path == "/login" {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	if _, err := client.PostForm(base+"/auth/login", url.Values{"email": {"admin@meridian.dev"}, "password": {adminPass}}); err != nil {
		t.Fatalf("login: %v", err)
	}
	for _, p := range []string{"/app", "/app/customers", "/app/invoices", "/app/subscriptions", "/app/customers/create", "/app/invoices/create", "/app/subscriptions/create"} {
		if code, body := e2eDo(t, client, "GET", base+p, ""); code != http.StatusOK {
			t.Errorf("authed screen %s = %d, want 200", p, code)
		} else if len(body) < 120 {
			t.Errorf("authed screen %s body suspiciously short (%d bytes)", p, len(body))
		}
	}

	// Full CRUD lifecycle for customers through its API + form screens.
	code, body := e2eDo(t, client, "POST", base+"/api/customers", `{"name": "e2e-name", "email": "e2e-email@example.com"}`)
	if code != http.StatusCreated {
		t.Fatalf("create customers = %d, want 201: %s", code, body)
	}
	id := e2eExtractID(body)
	if id == "" {
		t.Fatalf("create customers: no id in response: %s", body)
	}
	if code, body := e2eDo(t, client, "GET", base+"/app/customers/create", ""); code != http.StatusOK || !strings.Contains(body, "<form") {
		t.Errorf("create form customers = %d (has <form>? %v)", code, strings.Contains(body, "<form"))
	}
	if code, body := e2eDo(t, client, "GET", base+"/app/customers"+"/"+id, ""); code != http.StatusOK {
		t.Errorf("detail customers = %d, want 200", code)
	} else if !strings.Contains(body, "e2e-name") {
		t.Errorf("detail customers missing created value")
	}
	// The record page holds the edit form, prefilled; /edit is gone.
	if code, body := e2eDo(t, client, "GET", base+"/app/customers"+"/"+id, ""); code != http.StatusOK || !strings.Contains(body, "<form") || !strings.Contains(body, `value="e2e-name"`) {
		t.Errorf("record page customers = %d (has prefilled edit form? %v)", code, strings.Contains(body, `value="e2e-name"`))
	}
	if code, _ := e2eDo(t, client, "GET", base+"/app/customers"+"/"+id+"/edit", ""); code != http.StatusNotFound {
		t.Errorf("GET /app/customers/<id>/edit = %d, want 404: the record page holds the edit form", code)
	}
	if code, body := e2eDo(t, client, "PUT", base+"/api/customers"+"/"+id, `{"name": "e2e-updated"}`); code/100 != 2 {
		t.Errorf("update customers = %d: %s", code, body)
	}
	if code, _ := e2eDo(t, client, "DELETE", base+"/api/customers"+"/"+id, ""); code/100 != 2 {
		t.Errorf("delete customers = %d, want 2xx", code)
	}
	if code, _ := e2eDo(t, client, "GET", base+"/api/customers"+"/"+id, ""); code != http.StatusNotFound {
		t.Errorf("get deleted customers = %d, want 404", code)
	}

	// Scoping: an anonymous write to the access-/owner-scoped customers API is refused.
	if code, _ := e2eDo(t, http.DefaultClient, "POST", base+"/api/customers", `{"name": "e2e-name", "email": "e2e-email@example.com"}`); code != http.StatusUnauthorized && code != http.StatusForbidden {
		t.Errorf("anonymous write to customers = %d, want 401/403", code)
	}
}

func e2eFreeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func e2eWaitReady(t *testing.T, base string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		if r, err := http.DefaultClient.Do(req); err == nil {
			r.Body.Close()
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("server did not become ready")
}

// e2eDo runs one request and returns the status code + body. A non-empty
// body is sent as JSON. Network errors fail the test.
func e2eDo(t *testing.T, client *http.Client, method, u, body string) (int, string) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		t.Fatalf("%s %s: %v", method, u, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	r, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, u, err)
	}
	defer r.Body.Close()
	bb, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(bb)
}

var e2eIDRe = regexp.MustCompile(`"id"\s*:\s*"([^"]+)"`)

func e2eExtractID(body string) string {
	if m := e2eIDRe.FindStringSubmatch(body); len(m) == 2 {
		return m[1]
	}
	return ""
}
