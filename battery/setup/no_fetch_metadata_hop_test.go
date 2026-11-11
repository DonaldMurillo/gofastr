package setup

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// A browser sends Fetch Metadata only to a potentially trustworthy
// origin, so a setup link to a plain-HTTP LAN address
// (http://192.168.1.5:8080) clicked in a chat page arrives with no
// Sec-Fetch-Site and no Origin. Exchanged there, the token was spent on
// a navigation whose redirect the browser withholds the Strict cookie
// from: /setup answered 403 and every later click "already used". The
// arrival gets the hop instead, and the hop's reload exchanges.
func TestNoFetchMetadataClickHops(t *testing.T) {
	done := false
	r := buildTestRunner(t, false, &done)
	h := r.Handler(func() {}, nil, nil)
	link := "/setup?token=" + r.token

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, link, nil))
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 0 {
		t.Fatalf("no-Fetch-Metadata click = %d with %d cookies, want the 200 hop that spends nothing", w.Code, len(w.Result().Cookies()))
	}
	reload := link + "&" + handler.SameSiteHopParam + "=1"
	if got := w.Header().Get("Refresh"); got != "0;url="+reload {
		t.Fatalf("hop Refresh = %q, want 0;url=%s", got, reload)
	}

	// The reload carries no Fetch Metadata either; the marker ends the hop.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, reload, nil))
	if w.Code != http.StatusSeeOther || len(w.Result().Cookies()) == 0 {
		t.Fatalf("hop reload = %d, want the exchange's 303 with the cookie", w.Code)
	}
	if loc := w.Header().Get("Location"); strings.Contains(loc, "token=") {
		t.Fatalf("exchange Location %q still carries the token", loc)
	}
}
