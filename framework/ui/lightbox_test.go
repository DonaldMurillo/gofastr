package ui

import (
	"strings"
	"testing"
)

func TestLightboxRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Lightbox without Name should panic")
		}
	}()
	Lightbox(LightboxConfig{Images: []LightboxImage{{Src: "/a.jpg", Alt: "A"}}})
}

func TestLightboxRequiresImages(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Lightbox without Images should panic")
		}
	}()
	Lightbox(LightboxConfig{Name: "g1"})
}

func TestLightboxImageRequiresAlt(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("LightboxImage without Alt should panic")
		}
	}()
	Lightbox(LightboxConfig{Name: "g1", Images: []LightboxImage{{Src: "/a.jpg"}}})
}

func TestLightboxEmitsAnchorPerImage(t *testing.T) {
	thumbs, _ := Lightbox(LightboxConfig{
		Name: "gallery", Images: []LightboxImage{
			{Src: "/a.jpg", Alt: "A"},
			{Src: "/b.jpg", Alt: "B"},
			{Src: "/c.jpg", Alt: "C"},
		},
	})
	h := string(thumbs)
	if c := strings.Count(h, `data-fui-open="gallery"`); c != 3 {
		t.Errorf("expected 3 anchors with data-fui-open=gallery, got %d:\n%s", c, h)
	}
	if c := strings.Count(h, `data-fui-deeplink="`); c != 3 {
		t.Errorf("expected 3 deeplink anchors, got %d:\n%s", c, h)
	}
}

func TestLightboxDeeplinkCarriesSrcAndAlt(t *testing.T) {
	thumbs, _ := Lightbox(LightboxConfig{
		Name: "g", Images: []LightboxImage{{Src: "/photo.jpg", Alt: "A photo"}},
	})
	h := string(thumbs)
	// PathEscape (NOT QueryEscape) — runtime decodes with JS
	// decodeURIComponent which doesn't reverse '+' → space, so space
	// must be encoded as %20 not '+'. Confirm we don't slip back to
	// QueryEscape: a '+' anywhere in the encoded alt would re-break
	// the runtime decoder.
	if !strings.Contains(h, "src=%2Fphoto.jpg") {
		t.Errorf("deeplink should include path-encoded src:\n%s", h)
	}
	if !strings.Contains(h, "alt=A%20photo") {
		t.Errorf("deeplink should encode space as %%20 not '+':\n%s", h)
	}
	if strings.Contains(h, "alt=A+photo") {
		t.Errorf("regression: '+' in encoded alt — runtime decodeURIComponent doesn't reverse it:\n%s", h)
	}
}

func TestLightboxAnchorHrefFallback(t *testing.T) {
	thumbs, _ := Lightbox(LightboxConfig{
		Name: "g", Images: []LightboxImage{{Src: "/full.jpg", Alt: "F"}},
	})
	h := string(thumbs)
	if !strings.Contains(h, `href="/full.jpg"`) {
		t.Errorf("anchor should set href=Src for no-JS fallback:\n%s", h)
	}
}

func TestLightboxReturnsModalBuilder(t *testing.T) {
	_, modal := Lightbox(LightboxConfig{
		Name: "g1", Images: []LightboxImage{{Src: "/a.jpg", Alt: "A"}},
	})
	if modal == nil {
		t.Fatal("Lightbox should return a non-nil *widget.Builder")
	}
	def := modal.Definition()
	if def.Name != "g1" {
		t.Errorf("modal Name should match Lightbox Name, got %q", def.Name)
	}
	if !def.Hidden {
		t.Errorf("Lightbox modal should be Hidden by default (opened via data-fui-open)")
	}
}
