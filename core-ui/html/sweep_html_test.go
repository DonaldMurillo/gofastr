package html

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestHeadingAutoIDIsCappedAndUnescaped(t *testing.T) {
	got := string(Heading(HeadingConfig{Level: 2}, render.Text(strings.Repeat("word ", 40))))
	id := got[strings.Index(got, `id="heading-`)+len(`id="heading-`):]
	id = id[:strings.Index(id, `"`)]
	if len(id) > 64 || id == "" {
		t.Fatalf("auto id %q is %d bytes, want 1 to 64", id, len(id))
	}
	if got := string(Heading(HeadingConfig{Level: 2}, render.Text("Don't panic"))); !strings.Contains(got, `id="heading-don-t-panic"`) {
		t.Fatalf("escaped apostrophe leaked into the id: %s", got)
	}
}

func TestMainIsAFocusableSkipTarget(t *testing.T) {
	got := string(Main(MainConfig{}))
	if !strings.Contains(got, `id="main-content"`) || !strings.Contains(got, `tabindex="-1"`) {
		t.Fatalf("main = %s, want id main-content and tabindex -1", got)
	}
	if got := string(Main(MainConfig{ExtraAttrs: Attrs{"tabindex": "0"}})); strings.Contains(got, `tabindex="-1"`) {
		t.Fatalf("a caller's tabindex was overwritten: %s", got)
	}
}

func TestSafeExtraAttrsDropsDataFui(t *testing.T) {
	got := SafeExtraAttrs(Attrs{"data-fui-toggle": "x", "DATA-FUI-OPEN": "y", "data-testid": "keep"})
	if len(got) != 1 || got["data-testid"] != "keep" {
		t.Fatalf("SafeExtraAttrs = %v, want only data-testid", got)
	}
}

func TestImageAcceptsRasterDataButScriptDoesNot(t *testing.T) {
	const data = "data:image/png;base64,iVBORw0KGgo="
	if got := string(Image(ImageConfig{Src: data, Alt: "dot"})); !strings.Contains(got, `src="`+data+`"`) {
		t.Fatalf("img dropped a raster data: URI: %s", got)
	}
	if got := string(Script("data:text/javascript,alert(1)")); strings.Contains(got, "src=") {
		t.Fatalf("script kept a data: URI: %s", got)
	}
}
