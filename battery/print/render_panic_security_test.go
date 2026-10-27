package print

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type panickingDoc struct{}

func (panickingDoc) Render() render.HTML { panic("db secret") }

// A component that panicked while rendering had its error dropped: the
// fallback box, panic text included, was served as a 200 page or handed
// to the PDF renderer. It must fail the request the way a Build error
// does, with a generic 500 and no panic text.
func TestRenderPanicIsClean500(t *testing.T) {
	for _, route := range []string{"/print/doc", "/print/doc/pdf"} {
		t.Run(route, func(t *testing.T) {
			fr := &fakeRenderer{}
			b := New(Config{DefaultAccess: Public, PDFRenderer: fr}).Document(Document{
				Name: "doc", Path: "/doc",
				Build: func(*http.Request) (component.Component, error) { return panickingDoc{}, nil },
			})
			rec := get(t, mount(t, b), route)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "db secret") {
				t.Errorf("the panic text reached the client: %q", rec.Body.String())
			}
			if fr.gotHTML != "" {
				t.Errorf("the PDF renderer was handed the fallback page: %q", fr.gotHTML)
			}
		})
	}
}
