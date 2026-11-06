package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func TestDiffFinalNewlineAddsNoRow(t *testing.T) {
	for _, mode := range []ui.DiffMode{ui.DiffUnified, ui.DiffSplit} {
		for _, patch := range []string{"@@ -1 +1 @@\n-old\n+new", "@@ -1,2 +1,2 @@\n-old\n+new\n "} {
			config := ui.DiffViewerConfig{Patch: patch, Mode: mode}
			plain := ui.DiffViewer(config)
			config.Patch += "\n"
			terminated := ui.DiffViewer(config)
			if plain != terminated {
				t.Errorf("mode %q: a line terminator adds a phantom row", mode)
			}
		}
	}
	server := themeTestPageWithHead(t, "", string(ui.DiffViewer(ui.DiffViewerConfig{Patch: "@@ -1 +1 @@\n-old\n+new\n"})))
	ctx := moduleTestCtxURL(t, server.URL)
	var bodies []string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`[...document.querySelectorAll('.fui-diff-viewer__code')].map(node => node.textContent)`, &bodies)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(bodies, "|") != "old|new" {
		t.Fatalf("visible code lines = %q, want old and new only", bodies)
	}
}
