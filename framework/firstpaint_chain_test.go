package framework

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type firstPaintScreen struct{}

func (firstPaintScreen) Load(context.Context) error { return nil }
func (firstPaintScreen) Render() render.HTML {
	return render.HTML("<html><head><title>Home</title></head><body><h1>Home</h1></body></html>")
}
func (s firstPaintScreen) RenderCtx(context.Context) render.HTML { return s.Render() }

// TestFirstPaintHashesSurviveTheChain serves an inlined page through the
// App's whole default middleware chain. The host allows its inline blocks
// by patching the response's policy, which only works if the policy the
// security middleware set is the one the client receives with the
// patch: the timeout middleware once handed the page an empty header map,
// so the page inlined its head under a policy that never named the
// hashes and the browser refused every block.
func TestFirstPaintHashesSurviveTheChain(t *testing.T) {
	coreApp := app.NewApp("firstpaint")
	coreApp.RegisterScreen(app.NewScreen("/", &firstPaintScreen{}).WithTitle("Home"), nil)
	a, cleanup := startApp(t, NewUIHostApp(uihost.New(coreApp), WithConfig(AppConfig{Name: "firstpaint"})))
	defer cleanup()

	rec := httptest.NewRecorder()
	a.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body, csp := rec.Body.String(), rec.Header().Get("Content-Security-Policy")
	blocks := map[string]*regexp.Regexp{
		"style-src":  regexp.MustCompile(`(?s)<style id="cui-app-css">(.*?)</style>`),
		"script-src": regexp.MustCompile(`(?s)<head><script>(.*?)</script>`),
	}
	for directive, re := range blocks {
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no inline %s block; the page did not inline its head", directive)
		}
		sum := sha256.Sum256([]byte(m[1]))
		hash := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		if !strings.Contains(csp, directive+" ") || !strings.Contains(csp, hash) {
			t.Errorf("the policy the client receives does not allow the inline %s block %s:\n%s", directive, hash, csp)
		}
	}
}
