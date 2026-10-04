package uihost

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The parallel-parts HTTP fixtures (spike/layout-parts): a shell whose
// aside outlet defers, served by a real host so the session seam is
// the production one.

type partsPage struct{ html string }

func (p *partsPage) Render() render.HTML { return render.Text(p.html) }

type partsFill struct{ label string }

func (f *partsFill) Render() render.HTML { return render.Text("[" + f.label + "]") }

func newPartsHost() *UIHost {
	application := app.NewApp("Parts App")
	aside := app.NewOutlet("aside", app.OutletOptions{
		Deferred: true,
		Loading:  &app.Loading{Show: app.LoadingComponent(render.Text("LOADING"))},
	})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{aside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Primary(), l.Place(aside))
	})
	application.SetDefaultLayout(shell)
	application.RegisterScreen(app.NewScreen("/", &partsPage{html: "HOME"}).
		Fill(aside, &partsFill{label: "ASIDE"}), nil)
	application.RegisterScreen(app.NewScreen("/other", &partsPage{html: "OTHER"}).
		Fill(aside, &partsFill{label: "ASIDE-OTHER"}), nil)
	return New(application)
}

func partsServer(t *testing.T) *httptest.Server {
	t.Helper()
	ds := newPartsHost()
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	return srv
}

func partsClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar}
}

// TestParallelPartsMintOneSession: the page request of a navigation
// mints the session (a dead cookie re-mints, exactly like any partial);
// the part requests beside it NEVER mint — no Set-Cookie, no
// X-Gofastr-Session — or two Set-Cookies would race onto one
// navigation.
func TestParallelPartsMintOneSession(t *testing.T) {
	srv := partsServer(t)
	client := partsClient(t)

	// The page request, with a dead (absent) session cookie: it mints.
	req, _ := http.NewRequest("GET", srv.URL+"/other", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	req.Header.Set("X-Gofastr-Fills", "2")
	req.Header.Set("X-Gofastr-From", "/")
	req.Header.Set("X-Gofastr-Defer", "1")
	page, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()
	if page.Header.Get("Set-Cookie") == "" {
		t.Fatal("the page request must mint the session (a dead cookie re-mints)")
	}

	// The part request beside it, same client, live cookie: no mint.
	preq, _ := http.NewRequest("GET", srv.URL+"/other", nil)
	preq.Header.Set("X-Gofastr-Navigate", "1")
	preq.Header.Set("X-Gofastr-Part", "l:shell#aside")
	part, err := client.Do(preq)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(part.Body)
	part.Body.Close()
	if part.Header.Get("Set-Cookie") != "" {
		t.Errorf("part minted a session (Set-Cookie %q); only the page request mints", part.Header.Get("Set-Cookie"))
	}
	if part.Header.Get("X-Gofastr-Session") != "" {
		t.Errorf("part carried X-Gofastr-Session %q; the page answer owns the rollover", part.Header.Get("X-Gofastr-Session"))
	}
	if !strings.Contains(string(body), "[ASIDE-OTHER]") {
		t.Errorf("part body = %q, want the aside fill", body)
	}

	// A part with a DEAD session never mints its way out: minting here
	// would race the page request's own Set-Cookie onto one navigation,
	// so the dead session is the reset instead.
	bare := &http.Client{}
	dreq, _ := http.NewRequest("GET", srv.URL+"/other", nil)
	dreq.Header.Set("X-Gofastr-Navigate", "1")
	dreq.Header.Set("X-Gofastr-Part", "l:shell#aside")
	dead, err := bare.Do(dreq)
	if err != nil {
		t.Fatal(err)
	}
	dead.Body.Close()
	if dead.Header.Get("Set-Cookie") != "" {
		t.Errorf("dead-session part minted (Set-Cookie %q); the reset owns the recovery", dead.Header.Get("Set-Cookie"))
	}
	if dead.Header.Get("X-Gofastr-Part-Reset") != "session" {
		t.Errorf("dead-session part X-Gofastr-Part-Reset = %q, want session (the retryable reason; the page beside it re-mints)",
			dead.Header.Get("X-Gofastr-Part-Reset"))
	}
}

// TestPartResetReloadsOnceHTTP: every whole-page outcome answers 409
// with X-Gofastr-Part-Reset and no body the client applies. The
// reload-once behaviour itself is the browser test of the same name in
// core-ui/runtime.
func TestPartResetReloadsOnceHTTP(t *testing.T) {
	srv := partsServer(t)
	client := partsClient(t)
	// Mint a live session through a page request.
	page, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	page.Body.Close()

	part := func(path, addr string, liveCookie bool) *http.Response {
		t.Helper()
		c := client
		if !liveCookie {
			c = &http.Client{}
		}
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		req.Header.Set("X-Gofastr-Part", addr)
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	for name, tc := range map[string]struct {
		path, addr string
		live       bool
		wantReason string
	}{
		"unknown route":        {"/nope", "l:shell#aside", true, "1"},
		"forged address":       {"/", "l:shell#nope", true, "1"},
		"non-deferred address": {"/", "l:shell#toolbar", true, "1"},
		"dead session":         {"/", "l:shell#aside", false, "session"},
	} {
		res := part(tc.path, tc.addr, tc.live)
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusConflict {
			t.Errorf("%s: part status = %d, want 409", name, res.StatusCode)
		}
		// `1` is the opaque reload; `session` names the one reason the
		// client repairs without a reload (re-request after the page
		// commit carries the fresh cookie).
		if res.Header.Get("X-Gofastr-Part-Reset") != tc.wantReason {
			t.Errorf("%s: X-Gofastr-Part-Reset = %q, want %q", name,
				res.Header.Get("X-Gofastr-Part-Reset"), tc.wantReason)
		}
		if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", name, cc)
		}
		if len(strings.TrimSpace(string(body))) > 0 && strings.Contains(string(body), "data-cui-fill") {
			t.Errorf("%s: the 409 body must carry nothing the client applies, got %q", name, body)
		}
	}
}
