package ui

import (
	"strings"
	"testing"
)

// A ~20px JPEG data URL, the shape framework/image.BlurHashDataURL returns.
const testPlaceholder = "data:image/jpeg;base64,/9j/4AAQSkZJRg=="

func TestOptimizedImagePlaceholderPaints(t *testing.T) {
	out := string(OptimizedImage(OptimizedImageConfig{
		Src: "/hero.jpg", Alt: "Hero", Width: 800, Height: 600,
		Placeholder: testPlaceholder,
	}))
	assertPlaceholderPainted(t, out)
}

func TestPipelineImagePlaceholderPaints(t *testing.T) {
	out := string(PipelineImage(PipelineImageConfig{
		Fallback: "/hero.jpg", Alt: "Hero", Width: 800, Height: 600,
		Sources:     []PipelineSource{{URL: "/hero.webp", Width: 800, Type: "image/webp"}},
		Placeholder: testPlaceholder,
	}))
	assertPlaceholderPainted(t, out)
	// The placeholder must precede the <picture> so the real image paints
	// over it in DOM order without needing a z-index.
	if strings.Index(out, "ui-image__lqip") > strings.Index(out, "<picture") {
		t.Error("placeholder must be emitted before <picture>")
	}
}

func assertPlaceholderPainted(t *testing.T, out string) {
	t.Helper()
	if !strings.Contains(out, `class="ui-image__lqip"`) {
		t.Fatalf("no placeholder element rendered: %s", out)
	}
	if !strings.Contains(out, testPlaceholder) {
		t.Errorf("placeholder src missing: %s", out)
	}
	if !strings.Contains(out, "ui-image--placeheld") {
		t.Errorf("root is missing the --placeheld class: %s", out)
	}
	// Decorative: must not be announced.
	if !strings.Contains(out, `aria-hidden="true"`) {
		t.Errorf("placeholder must be aria-hidden: %s", out)
	}
	if !strings.Contains(out, `role="presentation"`) {
		t.Errorf("placeholder must have role=presentation: %s", out)
	}
	// The dead attributes this feature replaced must be gone.
	if strings.Contains(out, "data-placeholder") || strings.Contains(out, "data-blurhash") {
		t.Errorf("dead placeholder attributes still emitted: %s", out)
	}
}

// A data: URI needs no network, so lazy-loading or async-decoding the
// placeholder can only make it paint later than the image it stands in for.
func TestPlaceholderIsNotLazyOrAsync(t *testing.T) {
	out := string(OptimizedImage(OptimizedImageConfig{
		Src: "/hero.jpg", Alt: "Hero", Width: 800, Height: 600,
		Placeholder: testPlaceholder,
	}))
	start := strings.Index(out, "ui-image__lqip")
	if start < 0 {
		t.Fatalf("no placeholder element rendered: %s", out)
	}
	lqip := out[start:]
	lqip = lqip[:strings.IndexByte(lqip, '>')]
	if strings.Contains(lqip, `loading="lazy"`) {
		t.Errorf("placeholder must not be lazy: %s", lqip)
	}
	if strings.Contains(lqip, `decoding="async"`) {
		t.Errorf("placeholder must not decode async: %s", lqip)
	}
	if !strings.Contains(lqip, `decoding="sync"`) {
		t.Errorf("placeholder should decode sync: %s", lqip)
	}
}

// A Placeholder arrives as data: a column an old upload wrote, a hash from
// another client. Bad values degrade to no placeholder; they never take the
// page down. Contrast with Src/Alt, which are caller-code bugs and panic.
func TestBadPlaceholderDegradesSilently(t *testing.T) {
	cases := []struct{ name, placeholder string }{
		{"raw blurhash", "LEHV6nWB2yk8pyo0adR*.7kCMdnj"},
		{"html data url", "data:text/html;base64,PHNjcmlwdD4="},
		{"svg data url", "data:image/svg+xml,<svg onload=alert(1)>"},
		{"javascript url", "javascript:alert(1)"},
		{"remote url", "https://evil.example.com/track.gif"},
		{"garbage", "not a url at all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out string
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("a bad placeholder must not panic, got %v", r)
					}
				}()
				out = string(OptimizedImage(OptimizedImageConfig{
					Src: "/hero.jpg", Alt: "Hero", Width: 8, Height: 6,
					Placeholder: tc.placeholder,
				}))
			}()
			if strings.Contains(out, "ui-image__lqip") {
				t.Errorf("bad placeholder %q was rendered: %s", tc.placeholder, out)
			}
			if strings.Contains(out, "ui-image--placeheld") {
				t.Errorf("bad placeholder %q still set --placeheld: %s", tc.placeholder, out)
			}
			if strings.Contains(out, tc.placeholder) {
				t.Errorf("bad placeholder %q leaked into output: %s", tc.placeholder, out)
			}
			// The real image must still render.
			if !strings.Contains(out, `src="/hero.jpg"`) {
				t.Errorf("real image lost: %s", out)
			}
		})
	}
}

func TestNoPlaceholderMeansNoExtraElement(t *testing.T) {
	out := string(OptimizedImage(OptimizedImageConfig{
		Src: "/hero.jpg", Alt: "Hero", Width: 8, Height: 6,
	}))
	if strings.Contains(out, "ui-image__lqip") || strings.Contains(out, "placeheld") {
		t.Errorf("unexpected placeholder markup: %s", out)
	}
}

// ─── W5: parity gaps between the two components ─────────────────────

func TestPipelineImageSanitizesFallback(t *testing.T) {
	out := string(PipelineImage(PipelineImageConfig{
		Fallback: "javascript:alert(1)", Alt: "x", Width: 8, Height: 6,
	}))
	if strings.Contains(out, "javascript:") {
		t.Errorf("unsafe Fallback survived: %s", out)
	}
	if !strings.Contains(out, "/__gofastr/blank.png") {
		t.Errorf("expected the blank-image stub as fallback: %s", out)
	}
}

func TestPipelineImageSanitizesSources(t *testing.T) {
	out := string(PipelineImage(PipelineImageConfig{
		Fallback: "/ok.jpg", Alt: "x", Width: 8, Height: 6,
		Sources: []PipelineSource{
			{URL: "javascript:alert(1)", Width: 320, Type: "image/webp"},
			{URL: "/good.webp", Width: 640, Type: "image/webp"},
		},
	}))
	if strings.Contains(out, "javascript:") {
		t.Errorf("unsafe source survived: %s", out)
	}
	if !strings.Contains(out, "/good.webp") {
		t.Errorf("safe source was dropped: %s", out)
	}
}

// A comma in a srcset URL splits it into bogus candidates. PipelineImage
// escaped these already; OptimizedImage did not.
func TestOptimizedImageEscapesSrcsetCommas(t *testing.T) {
	out := string(OptimizedImage(OptimizedImageConfig{
		Src: "/a.jpg", Alt: "x", Width: 8, Height: 6,
		Sources: []ImageSource{{URL: "/img/a,b.jpg", Width: 320}},
	}))
	if strings.Contains(out, "a,b.jpg") {
		t.Errorf("raw comma left in srcset: %s", out)
	}
	if !strings.Contains(out, "a%2Cb.jpg") {
		t.Errorf("comma should be percent-escaped: %s", out)
	}
}

// A generated image (an icon, a chart, a decoded placeholder shown on its
// own) is legitimately inlined as a data URI. The image sinks pre-filtered
// with the stricter Resource policy, which silently swapped it for the blank
// stub. The image looked broken rather than blocked.
func TestImageSrcAcceptsRasterDataURL(t *testing.T) {
	t.Run("OptimizedImage", func(t *testing.T) {
		out := string(OptimizedImage(OptimizedImageConfig{
			Src: testPlaceholder, Alt: "Generated", Width: 20, Height: 20,
		}))
		if strings.Contains(out, "/__gofastr/blank.png") {
			t.Errorf("data URI src was replaced with the blank stub: %s", out)
		}
		if !strings.Contains(out, testPlaceholder) {
			t.Errorf("data URI src missing: %s", out)
		}
	})
	t.Run("PipelineImage", func(t *testing.T) {
		out := string(PipelineImage(PipelineImageConfig{
			Fallback: testPlaceholder, Alt: "Generated", Width: 20, Height: 20,
		}))
		if strings.Contains(out, "/__gofastr/blank.png") {
			t.Errorf("data URI fallback was replaced with the blank stub: %s", out)
		}
	})
	t.Run("srcset", func(t *testing.T) {
		out := string(OptimizedImage(OptimizedImageConfig{
			Src: "/a.jpg", Alt: "x", Width: 20, Height: 20,
			Sources: []ImageSource{{URL: testPlaceholder, Width: 20}},
		}))
		if !strings.Contains(out, "srcset") || !strings.Contains(out, "data:image/jpeg") {
			t.Errorf("data URI source was dropped from srcset: %s", out)
		}
	})
}

// The looser image policy must not admit non-image data URIs anywhere.
func TestImageSrcRejectsNonRasterDataURL(t *testing.T) {
	out := string(OptimizedImage(OptimizedImageConfig{
		Src: "data:text/html,<script>alert(1)</script>", Alt: "x", Width: 8, Height: 6,
	}))
	if strings.Contains(out, "text/html") {
		t.Errorf("html data URI survived as an image src: %s", out)
	}
	if !strings.Contains(out, "/__gofastr/blank.png") {
		t.Errorf("expected the blank stub: %s", out)
	}
}

// Gallery is the third <img src> sink in this package and had the same
// stricter-policy bug as OptimizedImage and PipelineImage.
func TestGalleryThumbAcceptsRasterDataURL(t *testing.T) {
	out := string(Gallery(GalleryConfig{
		Items: []GalleryItem{{Src: "/full.jpg", Thumb: testPlaceholder, Alt: "Generated"}},
	}))
	if strings.Contains(out, "/__gofastr/blank.png") {
		t.Errorf("data URI thumb was replaced with the blank stub: %s", out)
	}
	if !strings.Contains(out, testPlaceholder) {
		t.Errorf("data URI thumb missing: %s", out)
	}
}

func TestGalleryThumbRejectsUnsafeScheme(t *testing.T) {
	out := string(Gallery(GalleryConfig{
		Items: []GalleryItem{{Src: "/full.jpg", Thumb: "javascript:alert(1)", Alt: "x"}},
	}))
	if strings.Contains(out, "javascript:") {
		t.Errorf("unsafe thumb survived: %s", out)
	}
	if !strings.Contains(out, "/__gofastr/blank.png") {
		t.Errorf("expected the blank stub: %s", out)
	}
}

// Avatar has no pre-filter of its own. It relies on html.Image's policy, so
// the upstream switch to urlsafe.ImageSource is what makes an inlined avatar
// work. Pinned here so a future tightening of html.Image does not silently
// regress it.
func TestAvatarAcceptsRasterDataURL(t *testing.T) {
	out := string(Avatar(AvatarConfig{Name: "Ada Lovelace", Src: testPlaceholder}))
	if !strings.Contains(out, testPlaceholder) {
		t.Errorf("data URI avatar was dropped: %s", out)
	}
}
