package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// seedInRender seeds the per-request value while drawing (RenderScreen
// runs no Load) and binds it.
type seedInRender struct{ component.ContextOnly }

func (seedInRender) RenderCtx(ctx context.Context) render.HTML {
	testCompany.Seed(ctx, "TenantCo")
	return testCompany.Bind(ctx, "span", map[string]string{"id": "co"})
}

// The navigation answer carries the signal seed the full document does,
// as handlePartialPage's #gofastr-signals-partial island: without it a
// value the render seeded never reaches the client, and the bound
// element keeps the previous page's value. It still mints nothing
// (TestRenderScreenFullArmMintsPartialDoesNot).
func TestRenderScreenPartialCarriesSignalSeed(t *testing.T) {
	ds := New(newRecoverApp())

	full := httptest.NewRecorder()
	ds.RenderScreen(full, httptest.NewRequest(http.MethodGet, "/guarded", nil), seedInRender{}, ScreenResponse{})
	if !strings.Contains(full.Body.String(), `id="gofastr-signals"`) || !strings.Contains(full.Body.String(), "TenantCo") {
		t.Fatalf("setup: full document lacks the seed:\n%s", full.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	req.Header.Set("X-Gofastr-Navigate", "1")
	part := httptest.NewRecorder()
	ds.RenderScreen(part, req, seedInRender{}, ScreenResponse{})
	body := part.Body.String()
	i := strings.Index(body, `id="gofastr-signals-partial"`)
	if i < 0 {
		t.Fatalf("navigation answer has no #gofastr-signals-partial seed:\n%s", body)
	}
	if j := strings.Index(body[i:], "</script>"); j < 0 || !strings.Contains(body[i:i+j], "TenantCo") {
		t.Errorf("partial seed does not carry the seeded value:\n%s", body)
	}
	if got := part.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("partial arm set cookies: %v", got)
	}
}
