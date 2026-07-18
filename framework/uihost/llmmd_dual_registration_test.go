package uihost

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// TestLLMMD_DualSlashRegistrationNoPanic covers issue #89: a group index
// aliased at both the slashless and trailing-slash path (/studio and
// /studio/) collapses to one llm.md route (/studio/llm.md). Registering it
// twice used to panic on a duplicate router pattern during mount — now the
// per-screen loop dedupes the collapsed route.
func TestLLMMD_DualSlashRegistrationNoPanic(t *testing.T) {
	a := app.NewApp("llmmd-dual")
	a.RegisterScreen(app.NewScreen("/studio", &mdPageScreen{}).WithTitle("Studio"), nil)
	a.RegisterScreen(app.NewScreen("/studio/", &mdPageScreen{}).WithTitle("Studio index"), nil)

	// New() mounts mountPageLLMMD; the dup collapse used to panic here.
	ds := New(a, WithPublicLLMMD())
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	if _, resp := getBody(t, srv.URL+"/studio/llm.md"); resp.StatusCode != 200 {
		t.Fatalf("/studio/llm.md → %d, want 200", resp.StatusCode)
	}
}
