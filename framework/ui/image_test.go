package ui

import (
	"strings"
	"testing"
)

func TestOptimizedImageRequiresSrc(t *testing.T) {
	defer func() { recover() }()
	OptimizedImage(OptimizedImageConfig{Alt: "x", Width: 1, Height: 1})
	t.Fatal("expected panic with empty Src")
}

func TestOptimizedImageRequiresAlt(t *testing.T) {
	defer func() { recover() }()
	OptimizedImage(OptimizedImageConfig{Src: "/x.png", Width: 1, Height: 1})
	t.Fatal("expected panic with empty Alt and non-decorative")
}

func TestOptimizedImageRequiresWidthAndHeight(t *testing.T) {
	defer func() { recover() }()
	OptimizedImage(OptimizedImageConfig{Src: "/x.png", Alt: "x"})
	t.Fatal("expected panic without Width/Height (CLS)")
}

func TestOptimizedImageEmitsLazyLoadingByDefault(t *testing.T) {
	h := OptimizedImage(OptimizedImageConfig{Src: "/x.png", Alt: "x", Width: 100, Height: 50})
	mustContain(t, h, `loading="lazy"`)
	mustContain(t, h, `decoding="async"`)
	mustContain(t, h, `width="100"`)
	mustContain(t, h, `height="50"`)
}

func TestOptimizedImageEagerLoading(t *testing.T) {
	h := OptimizedImage(OptimizedImageConfig{
		Src: "/x.png", Alt: "x", Width: 1, Height: 1, Eager: true,
	})
	mustContain(t, h, `loading="eager"`)
}

func TestOptimizedImageHighPriority(t *testing.T) {
	h := OptimizedImage(OptimizedImageConfig{
		Src: "/x.png", Alt: "x", Width: 1, Height: 1, HighPriority: true,
	})
	mustContain(t, h, `fetchpriority="high"`)
}

func TestOptimizedImagePictureWithSrcset(t *testing.T) {
	h := OptimizedImage(OptimizedImageConfig{
		Src: "/x@1x.png", Alt: "x", Width: 200, Height: 100,
		Sources: []ImageSource{
			{URL: "/x@1x.png", Width: 200},
			{URL: "/x@2x.png", Width: 400},
		},
	})
	mustContain(t, h, "<picture")
	mustContain(t, h, `srcset="/x@1x.png 200w, /x@2x.png 400w"`)
	mustContain(t, h, `sizes="100vw"`)
}

func TestOptimizedImageCustomSizes(t *testing.T) {
	h := OptimizedImage(OptimizedImageConfig{
		Src: "/a.png", Alt: "x", Width: 1, Height: 1,
		Sources: []ImageSource{{URL: "/a.png", Width: 200}},
		Sizes:   "(min-width: 768px) 50vw, 100vw",
	})
	mustContain(t, h, `sizes="(min-width: 768px) 50vw, 100vw"`)
}

func TestOptimizedImageAspectAndFitClasses(t *testing.T) {
	h := OptimizedImage(OptimizedImageConfig{
		Src: "/a.png", Alt: "x", Width: 1, Height: 1,
		Aspect: ImageAspect16x9, Fit: ImageFitContain, Rounded: true,
	})
	for _, want := range []string{"ui-image--aspect-16-9", "ui-image--fit-contain", "ui-image--rounded"} {
		mustContain(t, h, want)
	}
}

func TestOptimizedImageDecorativeAllowsEmptyAlt(t *testing.T) {
	// Opt-in via class hook — should not panic.
	h := OptimizedImage(OptimizedImageConfig{
		Src: "/a.png", Alt: "", Width: 1, Height: 1, Class: "ui-image--decorative",
	})
	if !strings.Contains(string(h), `alt=""`) {
		t.Fatalf("decorative image must keep empty alt:\n%s", h)
	}
}
