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

func TestPipelineImageOnlyWidthMissing(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Height: 200,
	})
	out := string(h)
	if strings.Contains(out, `width=`) {
		t.Errorf("Width=0 should be omitted; got width attribute: %q", out)
	}
	if !strings.Contains(out, `height="200"`) {
		t.Errorf("Height=200 should be emitted: %q", out)
	}
}

func TestPipelineImageOnlyHeightMissing(t *testing.T) {
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 300,
	})
	out := string(h)
	if !strings.Contains(out, `width="300"`) {
		t.Errorf("Width=300 should be emitted: %q", out)
	}
	if strings.Contains(out, `height=`) {
		t.Errorf("Height=0 should be omitted; got height attribute: %q", out)
	}
}

func TestPipelineImageZeroDimensionsRendersGracefully(t *testing.T) {
	// User-generated content sometimes has missing width/height (old DB
	// rows pre-migration, malformed uploads). The component must not
	// panic — that crashes the render pipeline. Instead it emits an
	// <img> without intrinsic dimensions and accepts the CLS cost.
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("zero dims should not panic; got %v", r)
		}
	}()
	h := PipelineImage(PipelineImageConfig{Fallback: "/x.jpg", Alt: "x"})
	out := string(h)
	if !strings.Contains(out, "/x.jpg") {
		t.Errorf("expected fallback src in output, got %q", out)
	}
	// Should NOT have width="0" or height="0" — omit instead.
	if strings.Contains(out, `width="0"`) || strings.Contains(out, `height="0"`) {
		t.Errorf("zero dims should be omitted, not emitted; got %q", out)
	}
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

// TestPipelineSourcesFromHeaders pins the typed-bridge helper: given
// the headers from a VariantSet pipeline and a URL function, produce
// the PipelineSource slice without re-deriving MIME or width from
// filenames in caller code.
func TestPipelineSourcesFromHeaders(t *testing.T) {
	headers := []HeaderInfo{
		{Name: "photo-sm.jpg", Width: 320, MIME: "image/jpeg"},
		{Name: "photo-md.webp", Width: 800, MIME: "image/webp"},
	}
	got := PipelineSourcesFromHeaders(headers, func(name string) string {
		return "/uploads/" + name
	})
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	if got[0] != (PipelineSource{URL: "/uploads/photo-sm.jpg", Width: 320, Type: "image/jpeg"}) {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1] != (PipelineSource{URL: "/uploads/photo-md.webp", Width: 800, Type: "image/webp"}) {
		t.Errorf("got[1] = %+v", got[1])
	}
}

func TestPipelineImageDedupesIdenticalSources(t *testing.T) {
	// Two PipelineSource entries with the same (URL, Width, Type)
	// must collapse to one srcset candidate — duplicates are invalid
	// per the HTML spec ("each candidate must have a unique width
	// descriptor within the same source").
	h := PipelineImage(PipelineImageConfig{
		Fallback: "/p.jpg", Alt: "x", Width: 1, Height: 1,
		Sources: []PipelineSource{
			{URL: "/a.webp", Width: 100, Type: "image/webp"},
			{URL: "/a.webp", Width: 100, Type: "image/webp"},
		},
	})
	mustContain(t, h, "/a.webp 100w")
	if c := strings.Count(string(h), "/a.webp 100w"); c != 1 {
		t.Errorf("expected one srcset candidate, got %d", c)
	}
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
