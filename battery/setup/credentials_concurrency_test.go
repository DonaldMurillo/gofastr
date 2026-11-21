package setup

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestCredentialsDuringTokenExchange(t *testing.T) {
	complete := false
	r := buildTestRunner(t, false, &complete)
	const setupURL = "http://localhost/setup"
	for range 30 {
		h := r.Handler(nil, nil, nil)
		exchangeURL := r.SetupURL("http", "localhost")
		start := make(chan struct{})
		var exchange, wizard int
		var requests sync.WaitGroup
		requests.Go(func() {
			<-start
			req := httptest.NewRequest(http.MethodGet, exchangeURL, nil)
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			exchange = rec.Code
		})
		requests.Go(func() {
			<-start
			wizard = doGet(h, "/setup").Code
		})
		requests.Go(func() {
			<-start
			for range 100 {
				if got := r.SetupURL("http", "localhost"); got != setupURL && got != exchangeURL {
					t.Errorf("SetupURL during exchange = %q", got)
				}
			}
		})
		close(start)
		requests.Wait()
		if exchange != http.StatusSeeOther || wizard != http.StatusForbidden {
			t.Fatalf("exchange status = %d, wizard status = %d", exchange, wizard)
		}
	}
}
