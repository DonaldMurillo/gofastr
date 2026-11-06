package ui_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

func TestDiffHeadersStayOutsideHunks(t *testing.T) {
	patch := "--- a/first.sql\n+++ b/first.sql\n@@ -1,2 +1,2 @@\n--- SQL comment\n---- separator\n+++ increment\n++++ expression\n--- a/second.txt\n+++ b/second.txt\n@@ -1 +1 @@\n--- another removal\n+++ another addition"
	for _, mode := range []ui.DiffMode{ui.DiffUnified, ui.DiffSplit} {
		t.Run(string(mode), func(t *testing.T) {
			server := themeTestPageWithHead(t, "", string(ui.DiffViewer(ui.DiffViewerConfig{Patch: patch, Mode: mode})))
			ctx := moduleTestCtxURL(t, server.URL)
			var files int
			var code []string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.querySelectorAll('.fui-diff-viewer__file').length`, &files),
				chromedp.Evaluate(`[...document.querySelectorAll('.fui-diff-viewer__code')].map(node => node.textContent)`, &code),
			); err != nil {
				t.Fatal(err)
			}
			if files != 4 || len(code) != 6 {
				t.Fatalf("file rows = %d, code rows = %q; want four headers and six changed lines", files, code)
			}
			for _, want := range []string{"-- SQL comment", "--- separator", "++ increment", "+++ expression", "-- another removal", "++ another addition"} {
				found := false
				for _, body := range code {
					found = found || body == want
				}
				if !found {
					t.Errorf("missing changed code %q", want)
				}
			}
		})
	}
}
