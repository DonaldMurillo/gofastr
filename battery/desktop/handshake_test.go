package desktop

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Group 1: the boot handshake. Every refusal is proven by driving the
// real gate middleware + enter handler over HTTP.

// noRedirectClient returns a client that surfaces 30x responses
// instead of following them (the cookie and Location live on the
// redirect itself).
func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// serveHandshake serves the gate middleware around a plain next
// handler, WITH the enter handler mounted at its path, and pins the
// gate to the server's host.
func serveHandshake(t *testing.T, b *Battery) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(enterPath, b.enterHandler())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	srv := httptest.NewServer(b.gateMiddleware()(mux))
	t.Cleanup(srv.Close)
	// The gate is armed by Run; these tests exercise the armed posture.
	b.armGate()
	b.setHostPin(strings.TrimPrefix(srv.URL, "http://"))
	return srv
}

func doGet(t *testing.T, url, cookie string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	resp.Body.Close()
	return resp
}

func TestGateRefusesWithoutCookie(t *testing.T) {
	b, _ := newTestBattery(t)
	srv := serveHandshake(t, b)
	if resp := doGet(t, srv.URL+"/", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("no cookie: got %d, want 403", resp.StatusCode)
	}
}

func TestGateRefusesForgedCookieOfRightLength(t *testing.T) {
	b, _ := newTestBattery(t)
	srv := serveHandshake(t, b)
	// 32 bytes base64url = 43 chars: the same length as the real
	// session value, minted from the same CSPRNG.
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	forged := base64.RawURLEncoding.EncodeToString(raw[:])
	cookie := sessionCookieName + "=" + forged
	if resp := doGet(t, srv.URL+"/", cookie); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("forged cookie: got %d, want 403", resp.StatusCode)
	}
}

func TestGateRefusesWrongHost(t *testing.T) {
	b, _ := newTestBattery(t)
	srv := serveHandshake(t, b)
	cookie := sessionCookieName + "=" + b.sessionValue
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.Header.Set("Cookie", cookie)
	req.Host = "evil.example:8080"
	resp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong Host: got %d, want 403", resp.StatusCode)
	}
}

func TestGateAcceptsWithSessionCookie(t *testing.T) {
	b, _ := newTestBattery(t)
	srv := serveHandshake(t, b)
	cookie := sessionCookieName + "=" + b.sessionValue
	if resp := doGet(t, srv.URL+"/anything", cookie); resp.StatusCode != http.StatusOK {
		t.Fatalf("valid cookie: got %d, want 200", resp.StatusCode)
	}
}

func TestGateAcceptsStaleDuplicateCookieBeforeRealOne(t *testing.T) {
	b, _ := newTestBattery(t)
	srv := serveHandshake(t, b)
	// A stale pair in front of the real cookie must still authenticate:
	// honoring only cookies[0] would silently lock the window out.
	cookie := sessionCookieName + "=stalevalue; " + sessionCookieName + "=" + b.sessionValue
	if resp := doGet(t, srv.URL+"/", cookie); resp.StatusCode != http.StatusOK {
		t.Fatalf("stale duplicate cookie: got %d, want 200", resp.StatusCode)
	}
}

func TestGateArmingDecidesWhetherItGates(t *testing.T) {
	// Armed but unpinned: everything except /enter is refused even with
	// the right cookie (the window may answer before Run learns the
	// bound address; those requests must not slip through).
	b, _ := newTestBattery(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	})
	srv := httptest.NewServer(b.gateMiddleware()(mux))
	t.Cleanup(srv.Close)
	b.armGate()
	cookie := sessionCookieName + "=" + b.sessionValue
	if resp := doGet(t, srv.URL+"/", cookie); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("armed, before pin: got %d, want 403", resp.StatusCode)
	}

	// Unarmed (Run never called, the --serve host-independence mode):
	// the same battery is a pass-through. Locking an app out of its own
	// routes because a handshake that never happens never minted a
	// cookie is the bug the arming flag exists for.
	b2, _ := newTestBattery(t)
	srv2 := httptest.NewServer(b2.gateMiddleware()(mux))
	t.Cleanup(srv2.Close)
	if resp := doGet(t, srv2.URL+"/", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("unarmed: got %d, want 200 (serve mode pass-through)", resp.StatusCode)
	}
}

func TestEnterFlow(t *testing.T) {
	b, _ := newTestBattery(t)
	srv := serveHandshake(t, b)

	// Wrong/missing token → 403 with a plain body.
	resp := doGet(t, srv.URL+enterPath, "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("enter without token: got %d, want 403", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") == "application/json" {
		t.Error("enter refusal must be a plain one-line body, not an envelope")
	}

	// POST → 405.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+enterPath+"?t="+b.bootToken, nil)
	presp, err := noRedirectClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	presp.Body.Close()
	if presp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST enter: got %d, want 405", presp.StatusCode)
	}

	// Right token: 302 to / with the cookie flags pinned.
	resp = doGet(t, srv.URL+enterPath+"?t="+b.bootToken, "")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("enter with token: got %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Fatalf("enter redirect Location = %q, want /", loc)
	}
	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName {
		t.Fatalf("enter set cookies %+v, want exactly one %s", cookies, sessionCookieName)
	}
	c := cookies[0]
	if !c.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Error("session cookie must be SameSite=Strict")
	}
	if c.Secure {
		t.Error("session cookie must not be Secure (plain loopback)")
	}
	if c.MaxAge != 0 || !c.Expires.IsZero() {
		t.Error("session cookie must be a session cookie (no Max-Age, no Expires)")
	}
	if c.Path != "/" {
		t.Errorf("session cookie Path = %q, want /", c.Path)
	}

	// Replay: the right token a second time is 403.
	if resp := doGet(t, srv.URL+enterPath+"?t="+b.bootToken, ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("token replay: got %d, want 403", resp.StatusCode)
	}
}

func TestEnterSingleUseUnderConcurrency(t *testing.T) {
	b, _ := newTestBattery(t)
	// Drive the enter handler directly (no gate) so every request races
	// only the single-use flag.
	srv := httptest.NewServer(b.enterHandler())
	t.Cleanup(srv.Close)

	var wg sync.WaitGroup
	statuses := make([]int, 8)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp := doGet(t, srv.URL+enterPath+"?t="+b.bootToken, "")
			statuses[i] = resp.StatusCode
		}(i)
	}
	wg.Wait()
	found, forbidden := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusFound:
			found++
		case http.StatusForbidden:
			forbidden++
		}
	}
	if found != 1 || forbidden != len(statuses)-1 {
		t.Fatalf("single-use enter under race: %d accepted, %d refused; want exactly 1 accepted", found, forbidden)
	}
}
