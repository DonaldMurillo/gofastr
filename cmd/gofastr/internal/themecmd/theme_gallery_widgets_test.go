package themecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPreviewOpensGalleryWidgets(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	_, server := newBrowserThemeServer(t)
	for _, name := range []string{"site-demo-modal", "site-demo-drawer", "site-demo-bottomsheet", "ui-sidebar-drawer"} {
		t.Run(name, func(t *testing.T) {
			ctx := chromedptest.Context(t, chromedptest.WindowSize(600, 800))
			selector := fmt.Sprintf(`button[data-cui-open="%s"]`, name)
			visible := fmt.Sprintf(`(() => {
				const widget = document.querySelector('[data-cui-widget="%s"]');
				return !!widget && !widget.hidden && getComputedStyle(widget).display !== 'none'
					&& !!document.querySelector('[data-cui-backdrop="%s"]')
					&& widget.getAnimations({subtree: true}).every(animation => animation.playState !== 'running');
			})()`, name, name)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(server.URL+"/preview"),
				axetest.WaitReady(),
				chromedp.Click(selector, chromedp.ByQuery),
				chromedp.Poll(visible, nil, chromedp.WithPollingTimeout(5*time.Second)),
			); err != nil {
				t.Fatalf("gallery trigger did not open %s: %v", name, err)
			}
			if directory := os.Getenv("GOFASTR_VISUAL_DIR"); directory != "" {
				var screenshot []byte
				if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&screenshot)); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, name+".png"), screenshot, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
