package nojstest

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestSweepResetButtonIsNoSubmitAndScriptStaysOff(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/reset", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body><main><p>Reset only.</p>
<form action="/save" method="post"><input name="a"><input name="b"><button type="reset">Clear</button></form>
</main></body></html>`))
	})
	mux.HandleFunc("/clean", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(cleanPage))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	tab, cancel := NewTab(t, chromedptest.Context(t))
	defer cancel()
	ps, err := Check(tab, srv.URL+"/reset")
	if err != nil {
		t.Fatal(err)
	}
	if got := kinds(ps); !slices.Equal(got, []string{"form-no-submit"}) {
		t.Errorf("reset-only form: got %v, want [form-no-submit]", ps)
	}
	ps, err = Check(tab, srv.URL+"/clean")
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 0 {
		t.Errorf("second page in the same tab reported %v; script must stay off", ps)
	}
}
