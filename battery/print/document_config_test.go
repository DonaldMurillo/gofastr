package print

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestDocumentOwnsPageConfig(t *testing.T) {
	page := A4Portrait(MM(12))
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "receipt", Path: "/receipt", Page: &page,
		Build: func(*http.Request) (component.Component, error) { return stubDoc{}, nil },
	})
	page.Size = Letter
	r := router.New()
	if err := b.RegisterRoutes(r); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/print/receipt", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "size: A4;") {
		t.Fatalf("status = %d, missing declared page dimensions: %s", w.Code, w.Body)
	}
}

func TestDocumentTitlePanicReturnsServerError(t *testing.T) {
	b := New(Config{DefaultAccess: Public}).Document(Document{
		Name: "receipt", Path: "/receipt",
		Build:     func(*http.Request) (component.Component, error) { return stubDoc{}, nil },
		TitleFunc: func(*http.Request) string { panic("title callback failed") },
	})
	w := httptest.NewRecorder()
	b.serveHTML(b.docs[0]).ServeHTTP(w, httptest.NewRequest("GET", "/print/receipt", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "title callback failed") {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
}
