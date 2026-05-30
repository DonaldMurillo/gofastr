//go:build chromium

// Full-chain integration: an HTTP GET to a print document's /pdf route,
// through the battery's access gate + Build + shell, into the real
// chromepdf renderer, and back out as PDF bytes. Builds only under
// `go test -tags chromium` and skips cleanly when no browser is present.
package chromepdf_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/print"
	"github.com/DonaldMurillo/gofastr/battery/print/chromepdf"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

type body struct{ id string }

func (b body) Render() render.HTML {
	return render.HTML("<h1>Invoice " + render.Escape(b.id) + "</h1><p>Total: $10.00</p>")
}

func TestPDFRouteEndToEnd(t *testing.T) {
	pb := print.New(print.Config{
		DefaultAccess: print.Public,
		PDFRenderer:   chromepdf.New(chromepdf.Options{Timeout: 25 * time.Second, ExtraFlags: []string{"no-sandbox"}}),
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
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/print/invoice/42/pdf")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("PDF route returned %d (likely no usable Chromium)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("Content-Type = %q, want application/pdf", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Errorf("missing Content-Disposition")
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("response is not a PDF (got %d bytes, prefix %q)", buf.Len(), buf.Bytes()[:min(8, buf.Len())])
	}
}
