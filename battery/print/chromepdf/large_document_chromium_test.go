//go:build chromium

package chromepdf_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/print"
	"github.com/DonaldMurillo/gofastr/battery/print/chromepdf"
)

// The renderer used to navigate to the whole document as a base64
// data: URL. Chrome caps URLs at 2 MiB, so print HTML past about 1.5 MB
// failed with net::ERR_ABORTED and the /pdf route answered 502: a long
// invoice or report could never print.
func TestRenderPDFLargeDocument(t *testing.T) {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><title>big</title></head><body>")
	row := "<p>Line item: widget, quantity 1, unit price $10.00, total $10.00</p>\n"
	for b.Len() < 3<<20 {
		b.WriteString(row)
	}
	b.WriteString("</body></html>")

	r := chromepdf.New(chromepdf.Options{Timeout: 90 * time.Second, ExtraFlags: []string{"no-sandbox"}})
	pdf, err := r.RenderPDF(context.Background(), b.String(), print.PageConfig{Size: print.A4}, "")
	if err != nil {
		t.Fatalf("RenderPDF of a %d-byte document: %v", b.Len(), err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF (%d bytes, prefix %q)", len(pdf), pdf[:min(8, len(pdf))])
	}
}
