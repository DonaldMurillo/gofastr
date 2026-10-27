package uihost

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
)

// fetch decodes header bytes as Latin-1, so a raw UTF-8 target in
// X-Gofastr-Location reaches the router as /new/cafÃ©. The header is
// percent-encoded, as http.Redirect encodes Location on the full arm.
func TestPartialLocationIsASCII(t *testing.T) {
	application := app.NewApp("t")
	application.RegisterScreen(app.NewScreen("/new/café", &testHomeComp{}), nil)
	application.Redirect("/old", "/new/café")
	application.RegisterScreen(app.NewScreen("/dash", &testHomeComp{}).WithPolicy(
		app.PolicyFunc(func(context.Context) app.Decision { return decide.Redirect("/login?next=/docs/café") }),
	), nil)
	ds := New(application)

	for path, want := range map[string]string{
		"/old":  "/new/caf%C3%A9",
		"/dash": "/login?next=/docs/caf%C3%A9",
	} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("X-Gofastr-Navigate", "1")
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, req)
		if got := w.Header().Get("X-Gofastr-Location"); got != want {
			t.Errorf("%s: X-Gofastr-Location = %q, want %q", path, got, want)
		}
	}
}
