package framework

import (
	"io"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// What Start serves in front of the router (a server wrapper such as
// the service gate, and the setup wizard) answers in the caller's
// language: router middleware never runs for those requests, so the
// locale has to be on the context before them.
func TestServeOutsideRouterHasLocale(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "x.hello", i18n.Message{Text: "bonjour"})
	app := NewApp(WithI18n(i18n.NewTranslator(cat, "en")))
	app.WrapServer(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/gate" {
				_, _ = io.WriteString(w, i18n.FromContext(r.Context()).Tag)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	req, _ := http.NewRequest(http.MethodGet, "http://"+addr+"/gate", nil)
	req.Header.Set("Accept-Language", "fr-CA, en;q=0.5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "fr" {
		t.Fatalf("wrapper saw locale %q; want fr", body)
	}
}

// A locale resolver that panics in front of the router does not take
// the request down with it: the router's own recovery answers.
func TestServeLocaleResolverPanicRecovers(t *testing.T) {
	t.Setenv("GOFASTR_SETUP", "")
	t.Setenv("GOFASTR_ROLE", "")
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "x.hello", i18n.Message{Text: "bonjour"})
	app := NewApp(
		WithI18n(i18n.NewTranslator(cat, "en")),
		WithLocaleResolver(func(*http.Request) (string, bool) { panic("resolver") }),
	)
	addr, stop := startOnRandomPort(t, app)
	defer stop()

	resp, err := http.Get("http://" + addr + "/no-such-page")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500 from the router's recovery", resp.StatusCode)
	}
}
