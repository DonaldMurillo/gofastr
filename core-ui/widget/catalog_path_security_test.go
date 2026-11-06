package widget_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestCatalogUsesDecodedPagePath(t *testing.T) {
	for index, path := range []string{"/orders/a%2Fb", "/es/categor%C3%ADa", "/orders/a%252Fb"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		name := fmt.Sprintf("catalog-decoded-%d", index)
		definition := widget.New(name).Pages(request.URL.Path).Build()
		routes := router.New()
		widget.Mount(routes, &definition)
		widget.MountRuntime(routes)
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets?page="+url.QueryEscape(path), nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"`+name+`"`) {
			t.Errorf("catalog for %s omitted widget matching server path %q: %d %s", path, request.URL.Path, response.Code, response.Body.String())
		}
		response = httptest.NewRecorder()
		routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/__gofastr/widgets?page=%2Fbad%25zz", nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("malformed escaped page status = %d, want 400", response.Code)
		}
	}
}
