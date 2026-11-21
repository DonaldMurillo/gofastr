package print

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
)

var filenameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

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
// into PDF bytes.
type PDFRenderer interface {
	// RenderPDF converts the shelled HTML into a PDF.
	RenderPDF(ctx context.Context, html string, page PageConfig, baseURL string) ([]byte, error)
}

func (b *Battery) servePDF(doc *Document) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.cfg.PDFRenderer == nil {
			i18n.HTTPError(w, r, http.StatusNotImplemented, reasonPDFNotConfigured)
			return
		}

		if !b.admit(w, r, doc) {
			return
		}

		select {
		case b.renderSem <- struct{}{}:
			defer func() { <-b.renderSem }()
		default:
			w.Header().Set("Retry-After", renderBusyRetryAfter)
			i18n.HTTPError(w, r, http.StatusServiceUnavailable, reasonPDFBusy)
			return
		}

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
