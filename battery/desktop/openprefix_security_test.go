package desktop

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

// serveOpen is the armed gate and the local-user middleware around an
// app that answers with the request's user id, or "anonymous", with
// Config.OpenPrefixes = /api.
func serveOpen(t *testing.T) (*Battery, *httptest.Server) {
	t.Helper()
	shell := newFakeShell()
	b := New(Config{ID: "test.example.app", Title: "Test", Shell: shell, Logger: testLogger(t), OpenPrefixes: []string{"/api"}})
	b.grants = newMemGrantStore()
	b.user = localuser.ForID("local-user")
	who := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := handler.GetUser(r.Context()); ok {
			if lu, ok := u.(*localuser.User); ok {
				io.WriteString(w, lu.GetID())
				return
			}
		}
		io.WriteString(w, "anonymous")
	})
	srv := httptest.NewServer(b.gateMiddleware()(b.localUserMiddleware()(who)))
	t.Cleanup(srv.Close)
	b.armGate()
	b.setHostPin(strings.TrimPrefix(srv.URL, "http://"))
	return b, srv
}

func openDo(t *testing.T, method, url, host, cookie string, hdr map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// A local client with no window reaches an open path, and reaches it
// with no user: it is not the person at the keyboard just because it
// runs on the same machine.
func TestDesktopOpenPrefixPassesAnonymous(t *testing.T) {
	_, srv := serveOpen(t)
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		code, body := openDo(t, m, srv.URL+"/api/things", "", "", nil)
		if code != http.StatusOK || body != "anonymous" {
			t.Errorf("%s /api/things without the cookie = %d %q; want 200 anonymous", m, code, body)
		}
	}
}

func TestDesktopOpenPrefixLeavesEverythingElseGated(t *testing.T) {
	_, srv := serveOpen(t)
	for _, p := range []string{"/", "/apix", "/api//x", "/api%2F..%2Fadmin", "/__gofastr/desktop/bridge"} {
		if code, _ := openDo(t, http.MethodGet, srv.URL+p, "", "", nil); code != http.StatusForbidden {
			t.Errorf("GET %s without the cookie = %d; want 403", p, code)
		}
	}
}

func TestDesktopOpenPrefixKeepsHostPin(t *testing.T) {
	_, srv := serveOpen(t)
	if code, _ := openDo(t, http.MethodGet, srv.URL+"/api/things", "attacker.example:80", "", nil); code != http.StatusForbidden {
		t.Fatalf("open path with a rebinding Host = %d; want 403", code)
	}
}

func TestDesktopOpenPrefixRefusesCrossSiteWrite(t *testing.T) {
	_, srv := serveOpen(t)
	code, _ := openDo(t, http.MethodPost, srv.URL+"/api/things", "", "", map[string]string{"Sec-Fetch-Site": "cross-site"})
	if code != http.StatusForbidden {
		t.Fatalf("cross-site bodyless POST to an open path = %d; want 403", code)
	}
	// A preflighted cross-origin JSON call is CORS's to decide.
	code, _ = openDo(t, http.MethodPost, srv.URL+"/api/things", "", "", map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "application/json"})
	if code != http.StatusOK {
		t.Fatalf("cross-site JSON POST to an open path = %d; want 200", code)
	}
	// Its preflight (OPTIONS, no content type) must reach the app's CORS.
	code, _ = openDo(t, http.MethodOptions, srv.URL+"/api/things", "", "", map[string]string{
		"Sec-Fetch-Site": "cross-site", "Origin": "https://spa.example", "Access-Control-Request-Method": "POST"})
	if code != http.StatusOK {
		t.Fatalf("cross-site CORS preflight to an open path = %d; want it to reach the app", code)
	}
}

// The window's own fetches to /api keep the local identity.
func TestDesktopOpenPrefixWindowStaysSignedIn(t *testing.T) {
	b, srv := serveOpen(t)
	code, body := openDo(t, http.MethodGet, srv.URL+"/api/things", "", sessionCookieName+"="+b.sessionValue, nil)
	if code != http.StatusOK || body != "local-user" {
		t.Fatalf("window GET on an open path = %d %q; want 200 local-user", code, body)
	}
}

func TestDesktopOpenPrefixValidatedAtNew(t *testing.T) {
	for _, p := range []string{"/", "/__gofastr/desktop", "api", "/api/"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("New accepted OpenPrefixes %q", p)
				}
			}()
			New(Config{ID: "test.example.app", Title: "Test", Shell: newFakeShell(), Logger: testLogger(t), OpenPrefixes: []string{p}})
		}()
	}
}

func TestDesktopNewCopiesOpenPrefixes(t *testing.T) {
	prefixes := []string{"/api"}
	b := New(Config{ID: "test.example.app", Title: "Test", Shell: newFakeShell(), Logger: testLogger(t), OpenPrefixes: prefixes})
	prefixes[0] = "/"
	if b.cfg.OpenPrefixes[0] != "/api" {
		t.Fatalf("New aliased the caller's OpenPrefixes: %v", b.cfg.OpenPrefixes)
	}
}
