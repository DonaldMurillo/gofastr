package openapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corehandler "github.com/DonaldMurillo/gofastr/core/handler"
)

// A basePath is developer config, but it lands in an href: html escaping
// keeps it inside the attribute and does nothing about its scheme.
func TestDocsLinkRefusesUnsafeScheme(t *testing.T) {
	for _, base := range []string{"javascript:alert(1)//", "JavaScript:alert(1)//", "data:text/html,x"} {
		t.Run(base, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.URL.Path = base + "/"
			req = req.WithContext(corehandler.SetUser(req.Context(), "admin"))
			rec := httptest.NewRecorder()
			DocsHandler(NewSpec("test", "1.0"), base, false).ServeHTTP(rec, req)
			body := strings.ToLower(rec.Body.String())
			if strings.Contains(body, `href="javascript:`) || strings.Contains(body, `href="data:`) {
				t.Fatalf("docs page linked an unsafe scheme from basePath %q:\n%s", base, rec.Body.String())
			}
			if !strings.Contains(body, `href="/openapi.json"`) {
				t.Fatalf("docs page lost its spec link for basePath %q:\n%s", base, rec.Body.String())
			}
		})
	}
}
