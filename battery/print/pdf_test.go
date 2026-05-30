package print

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// fakeRenderer records what it was asked to render and returns canned PDF
// bytes, so tests can assert the PDF path without a real browser.
type fakeRenderer struct {
	gotHTML    string
	gotPage    PageConfig
	gotBaseURL string
	pdf        []byte
	err        error
}

func (f *fakeRenderer) RenderPDF(_ context.Context, html string, page PageConfig, baseURL string) ([]byte, error) {
	f.gotHTML, f.gotPage, f.gotBaseURL = html, page, baseURL
	if f.pdf == nil {
		f.pdf = []byte("%PDF-1.4 fake")
	}
	return f.pdf, f.err
}

func TestNotConfiguredReturns501(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "doc", Path: "/doc", Build: docBuild("x"),
	})
	rec := get(t, mount(t, b), "/print/doc/pdf")
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", rec.Code)
	}
}

func TestRendererInvoked(t *testing.T) {
	fr := &fakeRenderer{}
	b := New(Config{DefaultAccess: Public, PDFRenderer: fr}).Document(Document{
		Name: "invoice", Path: "/invoice/{id}",
		Page:  PageConfig{Size: Letter, Margin: MM(15)}.Ptr(),
		Build: docBuild("<p>x</p>"),
	})
	rec := get(t, mount(t, b), "/print/invoice/42/pdf")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if string(fr.pdf) != rec.Body.String() {
		t.Errorf("body != renderer output")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="invoice-42.pdf"`) {
		t.Errorf("Content-Disposition = %q, want filename invoice-42.pdf", cd)
	}
	if fr.gotPage.Size != Letter {
		t.Errorf("renderer page size = %q, want Letter", fr.gotPage.Size)
	}
	if !strings.HasPrefix(fr.gotHTML, "<!doctype html>") {
		t.Errorf("renderer did not receive shelled HTML")
	}
}

func TestPDFAppliesAccessGate(t *testing.T) {
	built := false
	fr := &fakeRenderer{}
	b := New(Config{PDFRenderer: fr}).Document(Document{ // default RequireAuth
		Name: "doc", Path: "/doc",
		Build: func(*http.Request) (component.Component, error) {
			built = true
			return stubDoc{html: "x"}, nil
		},
	})
	rec := get(t, mount(t, b), "/print/doc/pdf") // anonymous
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if built || fr.gotHTML != "" {
		t.Errorf("PDF path bypassed auth gate")
	}
}
