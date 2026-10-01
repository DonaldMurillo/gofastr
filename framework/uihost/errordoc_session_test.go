package uihost

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Error pages are real pages: every document that renders through the
// app's layout finishes through the same tail the normal page arm runs
// (finishPageDocument). Before that, a direct 404/500/405 load shipped
// chrome with NO session id: /__gofastr/widgets 401'd, and the first
// navigation away raced a part request against a session the page never
// minted (409 X-Gofastr-Part-Reset, full reload). Reproduced
type errdocPage struct{}

func (errdocPage) Render() render.HTML { return render.Text("HOME") }

func (errdocPage) SetParams(m map[string]string) {}

// newErrdocHost builds a host whose root layout is a tree layout, with
// a resolver-failing group for the 500 arm (an unavailable store fails
// the page in the policy phase, the tracker's LEG-40 shape).
func newErrdocHost() (*UIHost, *app.App) {
	application := app.NewApp("Errdoc App")
	shell := app.NewLayout("shell", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	application.SetDefaultLayout(shell)
	application.RegisterScreen(app.NewScreen("/", &errdocPage{}), nil)

	proj := app.NewLayout("project", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	project := app.NewKey[string]("project")
	group := app.NewScreenGroup("/boom/{project}", proj)
	group.Resolve(project.From(func(ctx context.Context) (string, error) {
		return "", errors.New("store unavailable")
	}))
	group.Requires(project)
	group.Screen(app.NewScreen("/boom/{project}", &errdocPage{}), nil)
	application.Router.ScreenGroup(group)
	return New(application), application
}

func errdocServer(t *testing.T) (*httptest.Server, *UIHost) {
	t.Helper()
	ds, _ := newErrdocHost()
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	return srv, ds
}

// chromeSessionID extracts the bare session id a document's SSE chrome
// carries (/__gofastr/sse?session=<id>), proving the id (not just any
// cookie) reached the page.
func chromeSessionID(body string) string {
	i := strings.Index(body, "session=")
	if i < 0 {
		return ""
	}
	rest := body[i+len("session="):]
	if j := strings.IndexAny(rest, "\"&<"); j >= 0 {
		rest = rest[:j]
	}
	if unescaped, err := url.QueryUnescape(rest); err == nil {
		return unescaped
	}
	return rest
}

// TestErrorDocumentMintsSession: the 404, 500 and 405 documents (and
// RenderScreen's full arm) each verify-or-mint the session exactly like
// a normal page — Set-Cookie on a cookie-less request, no-store,
// Vary: Cookie, and the session id in the SSE/widget chrome — and a
// live cookie is reused, not re-minted.
func TestErrorDocumentMintsSession(t *testing.T) {
	srv, ds := errdocServer(t)

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"not-found document", http.MethodGet, "/projects/unknown-project", http.StatusNotFound},
		{"error document", http.MethodGet, "/boom/legacy", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		body := string(raw)
		res.Body.Close()

		if res.StatusCode != tc.wantStatus {
			t.Errorf("%s: status = %d, want %d", tc.name, res.StatusCode, tc.wantStatus)
		}
		if got := res.Header.Values("Set-Cookie"); len(got) == 0 {
			t.Errorf("%s: no Set-Cookie on a cookie-less request; the error document must mint like a page", tc.name)
		}
		if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", tc.name, cc)
		}
		vary := strings.Join(res.Header.Values("Vary"), ", ")
		if !strings.Contains(vary, "Cookie") {
			t.Errorf("%s: Vary = %q, must carry Cookie", tc.name, vary)
		}
		sid := chromeSessionID(body)
		if sid == "" {
			continue
		}
		// The chrome id must belong to the token that was set: verify
		// it against the mint the response carried.
		var token string
		for _, c := range res.Cookies() {
			if c.Name == sessionCookieDevName || c.Name == sessionCookieSecureName {
				token = c.Value
			}
		}
		if id, ok := ds.verifySessionToken(token); !ok || id != sid {
			t.Errorf("%s: chrome session id %q does not match the Set-Cookie token (verified %q, ok=%v)", tc.name, sid, id, ok)
		}
	}
	// The 405 document: a screen path answers POST with the screen
	// itself, so the 405 arm is exercised directly the way its existing
	// test does (method_not_allowed_test.go), on the same layout host.
	rec405 := httptest.NewRecorder()
	rec405.Header().Set("Allow", "GET")
	ds.serveMethodNotAllowedPage(rec405, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec405.Code != http.StatusMethodNotAllowed {
		t.Errorf("405 document: status = %d, want 405", rec405.Code)
	}
	if got := rec405.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Error("405 document: no Set-Cookie on a cookie-less request")
	}
	if cc := rec405.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("405 document: Cache-Control = %q, want no-store", cc)
	}
	if !strings.Contains(strings.Join(rec405.Header().Values("Vary"), ", "), "Cookie") {
		t.Error("405 document: Vary must carry Cookie")
	}
	if chromeSessionID(rec405.Body.String()) == "" {
		t.Error("405 document: no session id in the SSE chrome")
	}

	// RenderScreen's full arm finishes through the same tail.
	ds2, _ := newErrdocHost()
	rec := httptest.NewRecorder()
	ds2.RenderScreen(rec, httptest.NewRequest(http.MethodGet, "/gone", nil), errdocPage{}, ScreenResponse{Status: http.StatusGone})
	if got := rec.Header().Values("Set-Cookie"); len(got) == 0 {
		t.Error("RenderScreen full arm: no Set-Cookie on a cookie-less request")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("RenderScreen full arm: Cache-Control = %q, want no-store", cc)
	}
	if chromeSessionID(rec.Body.String()) == "" {
		t.Error("RenderScreen full arm: no session id in the SSE chrome")
	}

	// A live cookie is reused: the same id in the chrome, no re-mint.
	sess := ds.CreateSession()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/projects/unknown-project", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: sess.Token})
	req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: sess.Token})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := io.ReadAll(res.Body)
	body := string(raw2)
	res.Body.Close()
	if got := res.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("live cookie re-minted: %v", got)
	}
	if id := chromeSessionID(body); id != sess.ID {
		t.Errorf("chrome session id = %q, want the live session %q", id, sess.ID)
	}
}

// TestErrorPartialArmsRunSessionRollover: the partial error arms
// (serveNotFoundPartial / serveErrorPartial) are reached only through
// handlePartialPage, which re-mints a dead session (Set-Cookie +
// X-Gofastr-Session) before any render — the same rollover the normal
// partial arm runs. This pins that the error PARTIAL never ships
// session-less either.
func TestErrorPartialArmsRunSessionRollover(t *testing.T) {
	srv, _ := errdocServer(t)

	for name, tc := range map[string]struct {
		path       string
		wantStatus int
	}{
		"not-found partial": {"/projects/unknown-project", http.StatusNotFound},
		"error partial":     {"/boom/legacy", http.StatusInternalServerError},
	} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+tc.path, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-From", "/")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != tc.wantStatus {
			t.Errorf("%s: status = %d, want %d", name, res.StatusCode, tc.wantStatus)
		}
		if res.Header.Get("X-Gofastr-Partial") != "true" {
			t.Errorf("%s: not partial-shaped (X-Gofastr-Partial = %q)", name, res.Header.Get("X-Gofastr-Partial"))
			continue
		}
		if got := res.Header.Values("Set-Cookie"); len(got) == 0 {
			t.Errorf("%s: no Set-Cookie; the partial error arm must run the same rollover as the normal partial", name)
		}
		if res.Header.Get("X-Gofastr-Session") == "" {
			t.Errorf("%s: no X-Gofastr-Session header naming the fresh stream id", name)
		}
	}
}
