package ui_test

import (
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// TestGalleryDropsDangerousHref pins that a Gallery anchor href never
// resolves to an executable scheme. Item Src (default + lightbox
// branches) and HrefFn output all flow through the headless primitive's
// anchor and image-source policies (urlsafe.CleanAnchor /
// urlsafe.ImageSource); javascript:/data:/vbscript:/ protocol-relative
// URLs are dropped so the thumbnail renders as a non-navigating figure
// instead of an XSS click target.
func TestGalleryDropsDangerousHref(t *testing.T) {
	dangerous := []string{
		"javascript:alert(document.cookie)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:MsgBox(1)",
		"//evil.example/x",
	}

	t.Run("default-src-branch", func(t *testing.T) {
		for _, p := range dangerous {
			h := ui.Gallery(ui.GalleryConfig{
				Items: []ui.GalleryItem{{Src: p, Alt: "x"}},
			})
			assertNoDangerHref(t, string(h), p)
		}
	})

	t.Run("hreffn-branch", func(t *testing.T) {
		for _, p := range dangerous {
			h := ui.Gallery(ui.GalleryConfig{
				HrefFn: func(i int, it ui.GalleryItem) string { return p },
				Items:  []ui.GalleryItem{{Src: "/safe.png", Alt: "x"}},
			})
			assertNoDangerHref(t, string(h), p)
		}
	})

	t.Run("safe-href-passes", func(t *testing.T) {
		h := ui.Gallery(ui.GalleryConfig{
			Items: []ui.GalleryItem{{Src: "/full.jpg", Alt: "x"}},
		})
		if !strings.Contains(string(h), `href="/full.jpg"`) {
			t.Fatalf("safe href dropped: %s", h)
		}
	})
}

func assertNoDangerHref(t *testing.T, out, payload string) {
	t.Helper()
	low := strings.ToLower(out)
	// The anchor href must not carry the dangerous value. We check the
	// canonical scheme tokens directly so a partial escape can't sneak by.
	for _, scheme := range []string{"javascript:", "vbscript:", "data:text/html"} {
		if strings.Contains(low, scheme) {
			t.Fatalf("dangerous scheme %q reached gallery output for payload %q:\n%s", scheme, payload, out)
		}
	}
	if strings.Contains(out, `href="//evil.example`) {
		t.Fatalf("protocol-relative href reached gallery output:\n%s", out)
	}
}

// An item whose Src the anchor policy refuses renders href="#". It must
// not also carry target=_blank, or a click opens a blank tab.
func TestGalleryRefusedSrcGetsNoNewTab(t *testing.T) {
	got := string(ui.Gallery(ui.GalleryConfig{Items: []ui.GalleryItem{
		{Src: "javascript:alert(1)", Alt: "bad"},
		{Src: "/ok.png", Alt: "good"},
	}}))
	bad, _, ok := strings.Cut(got, `</li>`)
	if !ok || !strings.Contains(bad, `alt="bad"`) {
		t.Fatalf("first item missing:\n%s", got)
	}
	if strings.Contains(bad, `target="_blank"`) {
		t.Errorf("refused Src still opens a new tab:\n%s", got)
	}
	if !strings.Contains(got, `href="/ok.png" rel="noopener" target="_blank"`) {
		t.Errorf("safe Src lost its new-tab link:\n%s", got)
	}
}
