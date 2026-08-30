package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// missingComp renders a body and asks for a 404: the route resolved, the
// entity behind it did not.
type missingComp struct{}

func (m *missingComp) Render() render.HTML { return render.HTML("<p>no such thing</p>") }
func (m *missingComp) ScreenStatusCode() int {
	return http.StatusNotFound
}

// A screen implementing ScreenStatusCode renders its body through the
// layout and still signals its own status.
func TestScreenStatusCodeOverridesOK(t *testing.T) {
	a := app.NewApp("status")
	a.Register("/gone", &missingComp{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/gone", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no such thing") {
		t.Error("body was not rendered through the layout")
	}
}

// A screen that does not implement the interface keeps 200.
func TestScreenWithoutStatusCodeStays200(t *testing.T) {
	a := app.NewApp("status")
	a.Register("/ok", &testHomeComp{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

// PageHandler forces its literal path into the dispatch, so a router route
// mounted elsewhere renders the registered screen's full page.
func TestPageHandlerRendersScreenAtForeignPath(t *testing.T) {
	a := app.NewApp("pagehandler")
	a.Register("/dash", &testHomeComp{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.PageHandler("/dash")(rec, httptest.NewRequest(http.MethodGet, "/elsewhere/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Home") {
		t.Errorf("screen body missing: %s", rec.Body.String())
	}
}

// A dynamic screen path is never forced into the dispatch: rewriting it
// would hand the literal pattern to param capture, so id would arrive as
// "{id}" and every param screen would 404.
func TestPageHandlerKeepsDynamicRequestPath(t *testing.T) {
	a := app.NewApp("pagehandler")
	a.Register("/thing/{id}", &paramJSONComp{}, nil)
	ds := New(a)

	rec := httptest.NewRecorder()
	ds.PageHandler("/thing/{id}")(rec, httptest.NewRequest(http.MethodGet, "/thing/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, ">42<") {
		t.Errorf("param not captured from the real path; got: %s", body)
	}
}
