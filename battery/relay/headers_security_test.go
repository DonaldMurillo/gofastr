package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// The embed grant is a bearer credential the host checks by possession,
// and boot-embed.js sends it on every same-origin fetch from an embed
// frame. It, and every other X-Gofastr-* header, is the app's own and
// never reaches the vendor.
func TestEmbedGrantNotForwarded(t *testing.T) {
	if !strings.HasPrefix(embed.GrantHeader, appHeaderPrefix) {
		t.Fatalf("embed.GrantHeader %q is outside the swept %q prefix", embed.GrantHeader, appHeaderPrefix)
	}
	base, cap := startRelayCap(t, defaultCfg)
	req, _ := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/x", nil)
	req.Header.Set(embed.GrantHeader, "grant-token")
	req.Header.Set("X-Gofastr-Nav", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	_, _, _, _, h := cap.snapshot()
	for _, name := range []string{embed.GrantHeader, "X-Gofastr-Nav"} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s reached upstream: %q", name, got)
		}
	}
}

// A vendor's Service-Worker-Allowed would let its script register a
// worker scoped past the mount, over every app page.
func TestServiceWorkerAllowedStripped(t *testing.T) {
	base := startRelay(t, defaultCfg, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Service-Worker-Allowed", "/")
		w.WriteHeader(http.StatusOK)
	})
	res, err := http.Get(base + "/__gofastr/t/e/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if got := res.Header.Values("Service-Worker-Allowed"); len(got) != 0 {
		t.Fatalf("Service-Worker-Allowed reached the browser: %q", got)
	}
}

// An upstream 103 Early Hints never reaches the browser (its headers
// skip modifyResponse, so an absolute Link preload would hand out the
// vendor origin), and the headers the host set before dispatch survive
// it: ReverseProxy clears the header map after writing a 1xx.
func TestEarlyHintsKeepHostHeaders(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://vendor.example/sdk.js>; rel=preload; as=script`)
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Del("Link")
		w.Header().Set("Content-Security-Policy", "script-src *")
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		router.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	var hints int
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		Got1xxResponse: func(int, textproto.MIMEHeader) error { hints++; return nil },
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/__gofastr/t/e/x", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if hints != 0 {
		t.Errorf("the browser got %d informational responses, want none", hints)
	}
	if got := res.Header.Values("Content-Security-Policy"); len(got) != 2 || got[0] != "default-src 'self'" || got[1] != "script-src *" {
		t.Errorf("Content-Security-Policy = %q, want the host's then the vendor's", got)
	}
	if got := res.Header.Values("X-Frame-Options"); len(got) != 1 || got[0] != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := res.Header.Values("Link"); len(got) != 0 {
		t.Errorf("Link = %q, want none", got)
	}
}
