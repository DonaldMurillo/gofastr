//go:build chromium

// These tests need a real Chromium/Chrome binary and only build under
// `go test -tags chromium`. They skip cleanly when no browser is found.
package chromepdf

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/print"
)

func TestChromeRendersMinimalPDF(t *testing.T) {
	r := New(Options{Timeout: 20 * time.Second})
	html := "<!doctype html><html><head><style>@page{size:A4;margin:10mm;}</style></head><body><h1>Hello</h1></body></html>"

	pdf, err := r.RenderPDF(context.Background(), html, print.A4Portrait(print.MM(10)), "http://localhost")
	if err != nil {
		t.Skipf("no usable Chromium for headless PDF (%v)", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF (prefix %q)", pdf[:min(8, len(pdf))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
