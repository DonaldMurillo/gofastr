package widget_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// The serve route treats a registered behaviour exactly like an
// embedded module: the real route (widget.MountRuntime) must serve it
// with the immutable year-long Cache-Control a content-addressed URL
// buys, return the module's served bytes verbatim, and 404 an unknown
// name instead of serving the page's HTML to a <script> tag.
func TestServeRuntimeModuleServesRegisteredBehavior(t *testing.T) {
	registry.IsolateForTest(t)
	const src = `(function () { 'use strict'; window.__serveProbe = true; })();`
	registry.RegisterBehavior("serve-probe", src, registry.Markers("[data-serve-probe]"))

	served, ok := runtime.Module("serve-probe")
	if !ok {
		t.Fatal("runtime.Module does not see the registered behaviour")
	}

	r := router.New()
	widget.MountRuntime(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	// The real route, with the ?v=<hash> the manifest carries.
	url := srv.URL + "/__gofastr/runtime/serve-probe.js?v=" + runtime.ModuleHash("serve-probe")
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("Content-Type = %q, want application/javascript", ct)
	}
	// The content-addressed URL is what buys the year-long immutable
	// cache; a registered module must get the same header as an
	// embedded one, or a host serves it uncached forever.
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want public, max-age=31536000, immutable", cc)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != served {
		t.Fatalf("body = %q (%d bytes), want the module's served bytes verbatim (%q)", string(body), len(body), served)
	}
	// An unknown name is a 404, not the page HTML or an empty 200.
	resp404, err := http.Get(srv.URL + "/__gofastr/runtime/no-such-behavior.js")
	if err != nil {
		t.Fatal(err)
	}
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown module status = %d, want 404", resp404.StatusCode)
	}
}
