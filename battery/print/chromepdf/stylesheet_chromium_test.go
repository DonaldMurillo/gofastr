//go:build chromium

package chromepdf_test

import (
	"bytes"
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
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// loopbackAssetFlags let the print document reach the test's loopback
// asset server. Chrome's Local Network Access check refuses a loopback
// subresource from the print document (a public-context page) on its
// own, independently of the resolver allow-list, so the tests switch
// it off to exercise the allow-list and the CSP. chromedp's default
// disable-features entries are repeated because this flag replaces them.
var loopbackAssetFlags = []string{
	"no-sandbox",
	"disable-features=site-per-process,Translate,BlinkGenPropertyTrees,LocalNetworkAccessChecks",
}

// assetServer serves app.css after delay and counts the requests that
// were answered in full.
func assetServer(t *testing.T, delay time.Duration) (origin, host string, served *atomic.Int32) {
	t.Helper()
	served = new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__gofastr/app.css" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(delay)
		w.Header().Set("Content-Type", "text/css")
		_, _ = io.WriteString(w, ":root { --color-text: #123456; }")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		served.Add(1)
	}))
	t.Cleanup(srv.Close)
	h, _, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return srv.URL, h, served
}

// The documented PDF configuration: BaseURL makes the app.css link
// absolute, and AllowedHosts lets Chromium reach BaseURL's host. The
// shell's meta CSP said style-src 'self', and the PDF document's origin
// is opaque (about:blank, a data: URL before it), so 'self' matched
// nothing and the stylesheet was refused even with the host allowed:
// PDFs never carried the app's tokens.
func TestPDFLoadsAppCSSFromBaseURL(t *testing.T) {
	origin, host, served := assetServer(t, 0)
	pb := print.New(print.Config{
		DefaultAccess: print.Public,
		BaseURL:       origin,
		AppCSSURL:     "/__gofastr/app.css",
		PDFRenderer: chromepdf.New(chromepdf.Options{
			Timeout:      25 * time.Second,
			ExtraFlags:   loopbackAssetFlags,
			AllowedHosts: []string{host},
		}),
	}).Document(print.Document{
		Name: "invoice", Path: "/invoice/{id}",
		Build: func(r *http.Request) (component.Component, error) {
			return body{id: router.Param(r, "id")}, nil
		},
	})
	r := router.New()
	if err := pb.RegisterRoutes(r); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	app := httptest.NewServer(r)
	defer app.Close()

	resp, err := http.Get(app.URL + "/print/invoice/42/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	pdf, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("PDF route: status %d, prefix %q", resp.StatusCode, pdf[:min(8, len(pdf))])
	}
	if served.Load() == 0 {
		t.Fatal("the PDF render never fetched app.css from BaseURL: the PDF has no design tokens")
	}
}

// Navigate used to wait for the load event before printing. Setting the
// document's content does not wait on its own, so the renderer waits for
// the same point: a stylesheet still in flight is loaded before the PDF
// is printed, not after. The delay is long on purpose: polling
// document.readyState instead printed about two seconds in, because it
// could read "complete" while the sheet was still pending.
func TestRenderPDFWaitsForStylesheet(t *testing.T) {
	origin, host, served := assetServer(t, 3*time.Second)
	doc := `<!doctype html><html><head><link rel="stylesheet" href="` + origin +
		`/__gofastr/app.css"></head><body><p>styled</p></body></html>`

	rd := chromepdf.New(chromepdf.Options{
		Timeout:      25 * time.Second,
		ExtraFlags:   loopbackAssetFlags,
		AllowedHosts: []string{host},
	})
	pdf, err := rd.RenderPDF(context.Background(), doc, print.PageConfig{Size: print.A4}, origin)
	if err != nil {
		t.Fatalf("RenderPDF: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF (prefix %q)", pdf[:min(8, len(pdf))])
	}
	if served.Load() == 0 {
		t.Fatal("RenderPDF returned before the document's stylesheet had loaded")
	}
}
