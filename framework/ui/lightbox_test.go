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
	Lightbox(LightboxConfig{})
}

func TestLightboxReturnsHiddenModalByName(t *testing.T) {
	b := Lightbox(LightboxConfig{Name: "photo-viewer"})
	if b == nil {
		t.Fatal("Lightbox should return non-nil *widget.Builder")
	}
	d := b.Definition()
	if d.Name != "photo-viewer" {
		t.Errorf("widget Name should match Lightbox Name; got %q", d.Name)
	}
	if !d.Hidden {
		t.Errorf("Lightbox modal should be Hidden by default (data-fui-open opens it)")
	}
}

func TestLightboxDeepLinkParams(t *testing.T) {
	b := Lightbox(LightboxConfig{Name: "x"})
	d := b.Definition()
	got := map[string]bool{}
	for _, p := range d.DeepLinkParams {
		got[p] = true
	}
	for _, want := range []string{"src", "alt", "caption", "group"} {
		if !got[want] {
			t.Errorf("Lightbox must declare DeepLinkParam %q", want)
		}
	}
}

func TestLightboxSlotRendersSignalBoundImg(t *testing.T) {
	slot := &lightboxSlot{name: "x", label: "Viewer"}
	h := string(slot.Render())
	if !strings.Contains(h, `data-fui-signal="src"`) {
		t.Errorf("slot should bind src signal:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-signal-mode="attr"`) {
		t.Errorf("slot src binding should be attr-mode:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-signal-attr="src"`) {
		t.Errorf("slot should mirror into the src attribute:\n%s", h)
	}
}

func TestLightboxNavArrowsAddsButtons(t *testing.T) {
	off := string((&lightboxSlot{name: "x", label: "x"}).Render())
	if strings.Contains(off, "ui-lightbox__nav--prev") {
		t.Errorf("NavArrows=false should NOT emit Prev/Next buttons:\n%s", off)
	}
	on := string((&lightboxSlot{name: "x", label: "x", navArrows: true}).Render())
	if !strings.Contains(on, "ui-lightbox__nav--prev") {
		t.Errorf("NavArrows=true should emit Prev button:\n%s", on)
	}
	if !strings.Contains(on, "ui-lightbox__nav--next") {
		t.Errorf("NavArrows=true should emit Next button:\n%s", on)
	}
	if !strings.Contains(on, `data-fui-lightbox-prev="x"`) {
		t.Errorf("Prev button should carry data-fui-lightbox-prev=<name>:\n%s", on)
	}
}

func TestLightboxShowCaptionAddsFigcaption(t *testing.T) {
	off := string((&lightboxSlot{name: "x", label: "x"}).Render())
	if strings.Contains(off, "<figcaption") {
		t.Errorf("ShowCaption=false should NOT emit <figcaption>:\n%s", off)
	}
	on := string((&lightboxSlot{name: "x", label: "x", showCaption: true}).Render())
	if !strings.Contains(on, "<figcaption") {
		t.Errorf("ShowCaption=true should emit <figcaption>:\n%s", on)
	}
	if !strings.Contains(on, `data-fui-signal="caption"`) {
		t.Errorf("figcaption should bind to caption signal:\n%s", on)
	}
}

func TestLightboxAllowDownloadAddsAnchor(t *testing.T) {
	off := string((&lightboxSlot{name: "x", label: "x"}).Render())
	if strings.Contains(off, "ui-lightbox__download") {
		t.Errorf("AllowDownload=false should NOT emit download anchor:\n%s", off)
	}
	on := string((&lightboxSlot{name: "x", label: "x", allowDownload: true}).Render())
	if !strings.Contains(on, `class="ui-lightbox__download"`) {
		t.Errorf("AllowDownload=true should emit download anchor:\n%s", on)
	}
	if !strings.Contains(on, `data-fui-signal-attr="href"`) {
		t.Errorf("download anchor should mirror src signal into href:\n%s", on)
	}
}

func TestLightboxLabelledByPointsToCaptionTitle(t *testing.T) {
	b := Lightbox(LightboxConfig{Name: "myview"})
	d := b.Definition()
	if d.LabelledBy != "myview-title" {
		t.Errorf("LabelledBy should point to <name>-title; got %q", d.LabelledBy)
	}
}
