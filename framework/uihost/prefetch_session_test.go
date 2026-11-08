package uihost

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost/internal/sessiontoken"
)

// A prefetch answer tells the runtime how long the session behind it
// has left, so the preload cache drops the entry when the session ends
// instead of painting a page whose islands and actions would 401. The
// token is minted a minute short of sessionMaxAge, the session's last
// minute.
func TestPrefetchCarriesSessionExpiry(t *testing.T) {
	application := app.NewApp("t")
	application.Register("/plain", &testHomeComp{}, nil)
	ds := New(application)
	token, _, err := sessiontoken.Mint(ds.sessionKey, time.Now().Add(-sessionMaxAge+time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	get := func(prefetch bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/plain", nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		if prefetch {
			req.Header.Set("X-Gofastr-Prefetch", "1")
		}
		req.AddCookie(&http.Cookie{Name: sessionCookieSecureName, Value: token})
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		return rec
	}
	rec := get(true)
	if rec.Code != http.StatusOK {
		t.Fatalf("prefetch with a live session: status %d, want 200", rec.Code)
	}
	left, err := strconv.Atoi(rec.Header().Get("X-Gofastr-Session-Expires-In"))
	if err != nil || left < 50 || left > 60 {
		t.Fatalf("X-Gofastr-Session-Expires-In = %q, want the session's last minute in seconds",
			rec.Header().Get("X-Gofastr-Session-Expires-In"))
	}
	// Only the prefetch cache reads it; a click's own fetch needs none.
	if h := get(false).Header().Get("X-Gofastr-Session-Expires-In"); h != "" {
		t.Errorf("a navigation partial carries X-Gofastr-Session-Expires-In %q, want none", h)
	}
}
