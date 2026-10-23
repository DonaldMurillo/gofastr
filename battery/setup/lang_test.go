package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// The wizard says its phrases in the reader's locale, so its document
// declares that language and direction rather than a fixed English.
func TestWizardDeclaresReaderLang(t *testing.T) {
	done := false
	h := buildTestRunner(t, true, &done).Handler(func() {}, nil, nil)
	for _, tc := range []struct{ tag, want string }{
		{"", `<html dir="ltr" lang="en">`},
		{"fr", `<html dir="ltr" lang="fr">`},
		{"ar", `<html dir="rtl" lang="ar">`},
	} {
		req := httptest.NewRequest(http.MethodGet, "/setup", nil)
		if tc.tag != "" {
			req = req.WithContext(i18n.WithContext(req.Context(), i18n.Locale{Tag: tc.tag}))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if body := w.Body.String(); !strings.Contains(body, tc.want) {
			t.Errorf("locale %q: want %s in %.120q", tc.tag, tc.want, body)
		}
	}
}
