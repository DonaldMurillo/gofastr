package ui

import (
	"strings"
	"testing"
)

func TestPipelineImageRequiresFallback(t *testing.T) {
	defer func() { recover() }()
	PipelineImage(PipelineImageConfig{Alt: "x", Width: 1, Height: 1})
	t.Fatal("expected panic with empty Fallback")
}

func TestPipelineImageRequiresAlt(t *testing.T) {
	defer func() { recover() }()
	PipelineImage(PipelineImageConfig{Fallback: "/x.jpg", Width: 1, Height: 1})
	t.Fatal("expected panic with empty Alt")
}

func TestPipelineImageRequiresWidthHeight(t *testing.T) {
	defer func() { recover() }()
	PipelineImage(PipelineImageConfig{Fallback: "/x.jpg", Alt: "x"})
	t.Fatal("expected panic without Width/Height")
}

func TestPipelineImageEmitsOneSourcePerType(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/photo-md.jpg",
		Alt:      "scenic",
		Width:    800, Height: 600,
		Sources: []PipelineSource{
			{URL: "/photo-sm.webp", Width: 320, Type: "image/webp"},
			{URL: "/photo-md.webp", Width: 800, Type: "image/webp"},
			{URL: "/photo-lg.webp", Width: 1600, Type: "image/webp"},
			{URL: "/photo-sm.jpg", Width: 320, Type: "image/jpeg"},
			{URL: "/photo-md.jpg", Width: 800, Type: "image/jpeg"},
			{URL: "/photo-lg.jpg", Width: 1600, Type: "image/jpeg"},
		},
	})
	out := string(h)
	mustContain(t, h, `type="image/webp"`)
	mustContain(t, h, `type="image/jpeg"`)
	mustContain(t, h, "/photo-sm.webp 320w, /photo-md.webp 800w, /photo-lg.webp 1600w")
	mustContain(t, h, "/photo-sm.jpg 320w, /photo-md.jpg 800w, /photo-lg.jpg 1600w")
	// WebP <source> must appear before JPEG <source> so legacy browsers
	// fall through correctly.
	if iWebP, iJPEG := strings.Index(out, "image/webp"), strings.Index(out, "image/jpeg"); iWebP < 0 || iJPEG < 0 || iWebP > iJPEG {
		t.Errorf("WebP source must precede JPEG source; got webp@%d jpeg@%d", iWebP, iJPEG)
	}
}

func TestPipelineImageEmitsPlaceholderDataURL(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 100, Height: 50,
		Placeholder: "data:image/jpeg;base64,Zm9v",
	})
	mustContain(t, h, `data-placeholder="data:image/jpeg;base64,Zm9v"`)
}

func TestPipelineImageEmitsBlurHashAttr(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 100, Height: 50,
		Placeholder: "LEHV6nWB2yk8pyo0adR*.7kCMdnj",
	})
	mustContain(t, h, `data-blurhash="LEHV6nWB2yk8pyo0adR*.7kCMdnj"`)
}

func TestPipelineImageHonoursSizes(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 100, Height: 50,
		Sources: []PipelineSource{{URL: "/p.webp", Width: 100, Type: "image/webp"}},
		Sizes:   "(min-width: 1024px) 1024px, 100vw",
	})
	mustContain(t, h, `sizes="(min-width: 1024px) 1024px, 100vw"`)
}

func TestPipelineImageDefaultsToLazyLoading(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 1, Height: 1,
	})
	mustContain(t, h, `loading="lazy"`)
}

func TestPipelineImageEagerFlipsLoading(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 1, Height: 1, Eager: true,
	})
	mustContain(t, h, `loading="eager"`)
}

func TestPipelineImageSortsByWidthWithinType(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 1, Height: 1,
		Sources: []PipelineSource{
			{URL: "/p-lg.webp", Width: 1600, Type: "image/webp"},
			{URL: "/p-sm.webp", Width: 320, Type: "image/webp"},
			{URL: "/p-md.webp", Width: 800, Type: "image/webp"},
		},
	})
	mustContain(t, h, "/p-sm.webp 320w, /p-md.webp 800w, /p-lg.webp 1600w")
}

func TestPipelineImageSkipsInvalidSources(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 1, Height: 1,
		Sources: []PipelineSource{
			{URL: "", Width: 100, Type: "image/webp"},        // empty URL
			{URL: "/p.webp", Width: 0, Type: "image/webp"},   // zero width
			{URL: "/p.webp", Width: 100, Type: ""},           // empty type
			{URL: "/p.webp", Width: 100, Type: "image/webp"}, // valid
		},
	})
	// Only the valid entry should appear in srcset.
	mustContain(t, h, "/p.webp 100w")
	out := string(h)
	if strings.Count(out, "<source ") != 1 {
		t.Errorf("expected exactly one <source>, got %d:\n%s", strings.Count(out, "<source "), out)
	}
}
