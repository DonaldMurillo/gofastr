package widget_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

// The catalog is answered per caller (uihost gates it on the session
// cookie, and what it lists depends on who asks), yet it went out with
// no Cache-Control and no Vary, so a shared cache could keep one
// caller's catalog and replay it to the next.
func TestCatalogIsNotStored(t *testing.T) {
	response := httptest.NewRecorder()
	widget.ServeWidgetList(response, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets?page=/", nil))
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Values("Vary"); len(got) != 1 || got[0] != "Cookie" {
		t.Errorf("Vary = %q, want Cookie", got)
	}
}
