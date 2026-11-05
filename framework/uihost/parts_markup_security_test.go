package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

func TestPartsRefuseStaleMarkup(t *testing.T) {
	for _, test := range []struct {
		markup string
		mode   string
		stale  bool
	}{
		{"1", "cors", true},
		{"future", "same-origin", true},
		{"", "no-cors", true},
		{runtime.MarkupVersion, "cors", false},
		{"", "", false},
	} {
		t.Run(test.markup+"/"+test.mode, func(t *testing.T) {
			host := newPartsHost()
			session := host.CreateSession()
			request := httptest.NewRequest(http.MethodGet, "/other", nil)
			request.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: session.Token})
			request.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: session.Token})
			request.Header.Set("X-Gofastr-Navigate", "1")
			request.Header.Set("X-Gofastr-Part", "l:shell#aside")
			request.Header.Set("X-Gofastr-Markup", test.markup)
			request.Header.Set("Sec-Fetch-Mode", test.mode)
			response := httptest.NewRecorder()
			host.ServeHTTP(response, request)
			if test.stale {
				if response.Code != http.StatusConflict || response.Header().Get("X-Gofastr-Part-Reset") != partResetAny || response.Body.Len() != 0 {
					t.Errorf("stale part: status=%d reset=%q body=%s", response.Code, response.Header().Get("X-Gofastr-Part-Reset"), response.Body)
				}
			} else if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "ASIDE-OTHER") {
				t.Errorf("compatible part: status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}
