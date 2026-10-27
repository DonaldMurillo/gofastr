package static

import (
	"context"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// The PWA offline page is an exported page like the rest: on a host that
// ignores _headers (GitHub Pages) only the meta carries the export's
// CSP, and the offline page shipped without one.
func TestPWAOfflinePageCarriesCSP(t *testing.T) {
	policy := "default-src 'self'; font-src 'self' https://fonts.gstatic.com; frame-ancestors 'none'"
	a := coreapp.NewApp("t")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{ThemeColor: "#112233"}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, ContentSecurityPolicy: policy}).Build(context.Background()); err != nil {
		t.Fatalf("Build: %v", err)
	}
	page := readOut(t, out, "__gofastr/pwa/offline/index.html")
	want := `<meta http-equiv="Content-Security-Policy" content="default-src 'self'; font-src 'self' https://fonts.gstatic.com">`
	if !strings.Contains(page, want) {
		t.Errorf("offline page lacks the export CSP meta %s:\n%s", want, page)
	}
}
