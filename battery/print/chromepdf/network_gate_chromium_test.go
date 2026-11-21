//go:build chromium

package chromepdf_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/print"
	"github.com/DonaldMurillo/gofastr/battery/print/chromepdf"
)

// RenderPDF installs the resolver gate itself, ahead of ExtraFlags: with no AllowedHosts the document reaches no server, and a host-resolver-rules entry in ExtraFlags replaces the gate.
func TestRenderPDFInstallsNetworkGate(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/css")
		_, _ = io.WriteString(w, "p { color: #123456; }")
	}))
	t.Cleanup(srv.Close)
	host := "gate-probe.localhost"
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	doc := `<!doctype html><html><head><link rel="stylesheet" href="http://` + host + `:` + port +
		`/x.css"></head><body><p>gated</p></body></html>`

	render := func(extra ...string) int32 {
		hits.Store(0)
		rd := chromepdf.New(chromepdf.Options{Timeout: 25 * time.Second, ExtraFlags: append(append([]string{}, loopbackAssetFlags...), extra...)})
		if _, err := rd.RenderPDF(context.Background(), doc, print.PageConfig{Size: print.A4}, ""); err != nil {
			t.Fatalf("RenderPDF: %v", err)
		}
		return hits.Load()
	}
	if n := render(); n != 0 {
		t.Fatalf("SECURITY: [ssrf] the default renderer fetched %d subresource(s) from %s", n, host)
	}
	if n := render("host-resolver-rules=MAP " + host + " 127.0.0.1"); n == 0 {
		t.Fatal("a host-resolver-rules entry in ExtraFlags did not replace the gate")
	}
}
