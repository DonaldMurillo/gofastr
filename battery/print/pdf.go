package print

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/DonaldMurillo/gofastr/core/i18n"
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
	// resolved page setup (paper size + margins). baseURL is
	// Config.BaseURL, the host-configured origin, never the request's
	// Host header; it is empty when unset. When it and AppCSSURL are
	// both set, the shelled HTML already carries the absolute app.css
	// link built from it.
	RenderPDF(ctx context.Context, html string, page PageConfig, baseURL string) ([]byte, error)
}

// servePDF returns the handler for a document's PDF route. When no
// PDFRenderer is configured it responds 501 Not Implemented so the host
// sees "capability unwired" distinctly from a missing document.
func (b *Battery) servePDF(doc *Document) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.cfg.PDFRenderer == nil {
			i18n.HTTPError(w, r, http.StatusNotImplemented, reasonPDFNotConfigured)
			return
		}

		// The access policy runs before a render slot is taken: a
		// request it refuses must not hold one while the policy looks
		// the caller up (RequireOwner's callback is a data load), or a
		// caller asking for other people's documents keeps the slots
		// busy and their owners get 503. A refused caller also learns
		// it was refused, not whether the renderer is busy.
		if !b.admit(w, r, doc) {
			return
		}

		// Bound the renders before Build: each RenderPDF call in the
		// shipped chromepdf backend is a whole headless-Chromium
		// process, so an uncapped route behind RequireAuth is a
		// process-exhaustion primitive. Past the battery-wide cap,
		// answer 503 with Retry-After instead of queueing another
		// renderer, and do it before Build, so a request that cannot
		// render now does not run the document's data load either.
		select {
		case b.renderSem <- struct{}{}:
			defer func() { <-b.renderSem }()
		default:
			w.Header().Set("Retry-After", renderBusyRetryAfter)
			i18n.HTTPError(w, r, http.StatusServiceUnavailable, reasonPDFBusy)
			return
		}

		// Reuse the exact same Build + shell as the HTML route, in PDF
		// mode (absolute app.css href, no auto-print).
		html, page, ok := b.build(w, r, doc, true)
		if !ok {
			return
		}

		pdf, err := b.cfg.PDFRenderer.RenderPDF(r.Context(), html, page, b.cfg.BaseURL)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadGateway, reasonPDFFailed)
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
