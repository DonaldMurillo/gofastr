package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func TestDiffCRLFMatchesLF(t *testing.T) {
	patch := "--- a/file\n+++ b/file\n@@ -1,2 +1,2 @@\n keep\n-old\n+new\n"
	for _, mode := range []ui.DiffMode{ui.DiffUnified, ui.DiffSplit} {
		config := ui.DiffViewerConfig{Patch: patch, Mode: mode}
		lf := ui.DiffViewer(config)
		config.Patch = strings.ReplaceAll(patch, "\n", "\r\n")
		crlf := ui.DiffViewer(config)
		if lf != crlf {
			t.Errorf("mode %q: CRLF changes rendered line bodies", mode)
		}
		server := themeTestPageWithHead(t, "", string(crlf))
		ctx := moduleTestCtxURL(t, server.URL)
		var bodies []string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('.fui-diff-viewer__code')].map(node => node.textContent)`, &bodies)); err != nil {
			t.Fatal(err)
		}
		for _, body := range bodies {
			if strings.ContainsAny(body, "\r\n") {
				t.Fatalf("mode %q: a line ending became visible preformatted content: %q", mode, bodies)
			}
		}
	}
}
