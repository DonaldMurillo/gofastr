package print

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// filenameUnsafe matches any character not allowed in a derived PDF
// filename stem. Replacing the whole class also removes CR/LF/NUL, so a
// param value can't smuggle bytes into the Content-Disposition header.
var filenameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeFilenameStem reduces s to a safe, trimmed filename stem.
func safeFilenameStem(s string) string {
	s = filenameUnsafe.ReplaceAllString(s, "-")
	return trimDashesDots(s)
}

func trimDashesDots(s string) string {
	for len(s) > 0 && (s[0] == '-' || s[0] == '.') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == '-' || s[len(s)-1] == '.') {
		s = s[:len(s)-1]
	}
	return s
}

// firstParamValue returns the sole route-param value when the route has
// exactly one (the common /{id} case), else "".
func firstParamValue(r *http.Request) string {
	params := router.Params(r)
	if len(params) != 1 {
		return ""
	}
	for _, v := range params {
		return v
	}
	return ""
}

// PDFRenderer turns a fully-assembled, standalone print HTML document
// into PDF bytes. It is the pluggable seam that keeps this package free
// of any headless-browser dependency: the chromedp-backed implementation
// lives in battery/print/chromepdf.
type PDFRenderer interface {
	// RenderPDF converts the shelled HTML into a PDF. page carries the
	// resolved page setup (paper size + margins). baseURL is the
	// scheme+host origin of the originating request, so an adapter can
	// resolve absolute resource links (e.g. the app.css stylesheet)
	// against the live host.
	RenderPDF(ctx context.Context, html string, page PageConfig, baseURL string) ([]byte, error)
}

// servePDF returns the handler for a document's PDF route. When no
// PDFRenderer is configured it responds 501 Not Implemented so the host
// sees "capability unwired" distinctly from a missing document.
func (b *Battery) servePDF(doc *Document) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.cfg.PDFRenderer == nil {
			http.Error(w, "PDF rendering is not configured for this app", http.StatusNotImplemented)
			return
		}

		// Reuse the exact same access gate + Build + shell as the HTML
		// route, in PDF mode (absolute app.css href, no auto-print).
		html, page, ok := b.build(w, r, doc, true)
		if !ok {
			return
		}

		pdf, err := b.cfg.PDFRenderer.RenderPDF(r.Context(), html, page, b.cfg.BaseURL)
		if err != nil {
			http.Error(w, "PDF rendering failed", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", pdfFilename(doc, r)))
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(pdf)
	})
}

// pdfFilename derives a download filename for a document. It prefers the
// document name and, when the route has a single {id}-style param, appends
// it (e.g. "invoice-42.pdf"). The result is sanitized to a safe stem.
func pdfFilename(doc *Document, r *http.Request) string {
	stem := safeFilenameStem(doc.Name)
	if id := firstParamValue(r); id != "" {
		stem += "-" + safeFilenameStem(id)
	}
	if stem == "" {
		stem = "document"
	}
	return stem + ".pdf"
}
