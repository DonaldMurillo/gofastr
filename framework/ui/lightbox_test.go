package ui

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
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
	if strings.Contains(off, "fui-lightbox__nav--prev") {
		t.Errorf("NavArrows=false should NOT emit Prev/Next buttons:\n%s", off)
	}
	on := string((&lightboxSlot{name: "x", label: "x", navArrows: true}).Render())
	if !strings.Contains(on, "fui-lightbox__nav--prev") {
		t.Errorf("NavArrows=true should emit Prev button:\n%s", on)
	}
	if !strings.Contains(on, "fui-lightbox__nav--next") {
		t.Errorf("NavArrows=true should emit Next button:\n%s", on)
	}
	if !strings.Contains(on, `data-fui-lightbox-prev="x"`) {
		t.Errorf("Prev button should carry data-fui-lightbox-prev=<name>:\n%s", on)
	}
	if !strings.Contains(on, `data-fui-lightbox-next="x"`) {
		t.Errorf("Next button should carry data-fui-lightbox-next=<name>:\n%s", on)
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
	if strings.Contains(off, "fui-lightbox__download") {
		t.Errorf("AllowDownload=false should NOT emit download anchor:\n%s", off)
	}
	on := string((&lightboxSlot{name: "x", label: "x", allowDownload: true}).Render())
	if !strings.Contains(on, `class="fui-lightbox__download"`) {
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
	if on := string((&lightboxSlot{name: "myview", label: "L"}).Render()); !strings.Contains(on, `id="myview-title"`) {
		t.Errorf("the title span the modal points at should exist:\n%s", on)
	}
}

func TestLightboxExtraAttrsOnViewerRoot(t *testing.T) {
	b := Lightbox(LightboxConfig{Name: "zoom", ExtraAttrs: map[string]string{
		"data-test":         "hook",
		"data-fui-lightbox": "spoof",
		"data-hui-lightbox": "spoof",
	}})
	body := string(b.Definition().Slots[0].Component.Render())
	root := body[:strings.Index(body, ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("viewer root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `data-fui-lightbox="zoom"`) {
		t.Errorf("owned lightbox wiring overridden by spoof:\n%s", root)
	}
	if strings.Contains(root, "data-hui-lightbox") {
		t.Errorf("the styled viewer carries a data-hui-lightbox* attribute — the hui family renders exactly when Wiring is zero, and a host module binding it here would double-bind the gallery the shipped module steps:\n%s", root)
	}
	if strings.Contains(root, "spoof") {
		t.Errorf("a forged data-fui-*/data-hui-* key reached the viewer root:\n%s", root)
	}
}

// TestLightboxSlotRootIsBare: the centered-panel chrome opts out
// through .fui-slot-bare as a whole class token on the slot's root
// element — the generic escape hatch, so the always-shipped panel CSS
// names no framework/ui component.
func TestLightboxSlotRootIsBare(t *testing.T) {
	body := string((&lightboxSlot{name: "x", label: "x"}).Render())
	root := body[:strings.Index(body, ">")+1]
	if !classTokenPresent(root, "fui-slot-bare") {
		t.Errorf("lightbox slot root missing fui-slot-bare token:\n%s", root)
	}
	if !classTokenPresent(root, "fui-lightbox") {
		t.Errorf("lightbox slot root missing fui-lightbox token:\n%s", root)
	}
}

// The viewer the styled Lightbox renders carries exactly ONE hook
// vocabulary: the framework module's data-fui-lightbox* wiring. The
// hui twins are suppressed on this path — a host module binding them
// beside the shipped module would double-bind the gallery it steps
// and fight its pinch-zoom on the same image — they render exactly
// when a host asks for the unwired anatomy (headless's own tests pin
// that direction). The image attribute is the pinch-zoom module's
// lookup — the class selector the module used before the move is
// gone, and this pins its replacement.
func TestLightboxViewerCarriesExactlyOneVocabulary(t *testing.T) {
	h := string((&lightboxSlot{name: "lb", label: "L", navArrows: true}).Render())
	for _, want := range []string{
		`data-fui-lightbox="lb"`,
		`data-fui-lightbox-nav="true"`,
		`data-fui-lightbox-image=""`,
		`data-fui-lightbox-prev="lb"`,
		`data-fui-lightbox-next="lb"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("viewer missing %s:\n%s", want, h)
		}
	}
	for _, twin := range []string{
		`data-hui-lightbox=`,
		`data-hui-lightbox-nav`,
		`data-hui-lightbox-image`,
		`data-hui-lightbox-prev`,
		`data-hui-lightbox-next`,
	} {
		if strings.Contains(h, twin) {
			t.Errorf("styled viewer carries the hui twin %s — one vocabulary per render, or a host module double-binds:\n%s", twin, h)
		}
	}
	if strings.Contains(h, `class="ui-lightbox__`) {
		t.Errorf("the pre-rename ui-lightbox__ class family is still on the viewer:\n%s", h)
	}
}

// The zoom target is one fact rendered by the component and looked up
// by the module; a rename on either side that misses the other leaves
// a zoom that never engages, silently. Both halves are in this
// package, so the agreement is pinned here: the module's zoom
// selectors name the attribute the viewer renders. The module half
// reads the source with comments stripped (the jsWithoutComments
// discipline from framework/headless's behaviour gates): the module's
// own prose names the attribute, and a mention that lives only in a
// comment cannot count as a lookup.
var lightboxJSLineComment = regexp.MustCompile(`//[^\n]*`)
var lightboxJSBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)

func lightboxJSWithoutComments() string {
	return lightboxJSLineComment.ReplaceAllString(
		lightboxJSBlockComment.ReplaceAllString(lightboxJS, " "), " ")
}

func TestLightboxZoomTargetAgreesBetweenMarkupAndModule(t *testing.T) {
	h := string((&lightboxSlot{name: "lb", label: "L"}).Render())
	if !strings.Contains(h, `data-fui-lightbox-image=""`) {
		t.Errorf("the viewer does not render the zoom target:\n%s", h)
	}
	code := lightboxJSWithoutComments()
	if !strings.Contains(code, "[data-fui-lightbox-image]") {
		t.Error("the module does not look the zoom target up by [data-fui-lightbox-image] — the attribute the viewer renders; a class selector here is the contract violation the move removed")
	}
	if strings.Contains(lightboxJS, ".fui-lightbox__full'") || strings.Contains(lightboxJS, `".fui-lightbox__full"`) {
		t.Error("the module targets the image by class — the registered-module contract binds by attribute, never by class")
	}
}

// ─── The behaviour registration ─────────────────────────────────────
//
// The module is this package's: the descriptor the kernel's bridge and
// scan read is declared here, not in the kernel's table. These pin the
// descriptor exactly; the cold-load regression that exercises it
// through a real browser lives in core-ui/runtime
// (lightbox_bridge_e2e_test.go), which parses this file's registration
// out of the source so the two cannot drift.

func TestLightboxBehaviorRegistration(t *testing.T) {
	e, ok := registry.LookupBehavior("lightbox")
	if !ok {
		t.Fatal("lightbox behaviour not registered — the module this package embeds is unreachable")
	}
	if want := []string{"[data-fui-lightbox]"}; !reflect.DeepEqual(e.Markers, want) {
		t.Errorf("markers = %v, want %v (the viewer's own attribute; data-fui-comp keeps its one job, fetching the sheet)", e.Markers, want)
	}
	if want := []string{"widgets"}; !reflect.DeepEqual(e.Requires, want) {
		t.Errorf("requires = %v, want %v (navigation re-opens the widget through openWidget)", e.Requires, want)
	}
	want := []registry.Interaction{
		{Event: "click", Selector: "[data-fui-lightbox-prev],[data-fui-lightbox-next]"},
		{Event: "keydown", Scope: "[data-fui-widget]:not([hidden]) [data-fui-lightbox]", Keys: []string{"ArrowLeft", "ArrowRight"}},
	}
	if !reflect.DeepEqual(e.Interactions, want) {
		t.Errorf("interactions = %#v, want %#v", e.Interactions, want)
	}
	if strings.Contains(e.Source, "data-fui-comp") || strings.Contains(e.Source, "ui-lightbox__full") {
		t.Errorf("the module still binds by the kernel's comp marker or by a class — the registered-module contract forbids both")
	}
}

// A malformed marker or interaction spec is refused at registration
// with the field's name, so a broken descriptor is a startup failure
// and never a silently lost click. One probe per shape, on an isolated
// registry: this package's real registration above is the valid case.
func TestLightboxRegistrationShapeIsRefusedWhenMalformed(t *testing.T) {
	cases := []struct {
		name string
		opts []registry.BehaviorOption
		want string
	}{
		{"class marker", []registry.BehaviorOption{registry.Markers(".fui-lightbox")}, "must be an attribute selector"},
		{"click without selector", []registry.BehaviorOption{
			registry.Markers("[data-hui-probe]"),
			registry.Interactions(registry.Interaction{Event: "click"})}, "Selector is empty"},
		{"keydown without scope", []registry.BehaviorOption{
			registry.Markers("[data-hui-probe]"),
			registry.Interactions(registry.Interaction{Event: "keydown", Keys: []string{"Enter"}})}, "Scope is empty"},
		{"unknown event", []registry.BehaviorOption{
			registry.Markers("[data-hui-probe]"),
			registry.Interactions(registry.Interaction{Event: "pointerdown", Selector: "[data-hui-probe]"})}, "not one the bridge retains"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("the malformed registration was accepted")
				}
				if !strings.Contains(strings.TrimSpace(stringifyPanic(r)), c.want) {
					t.Fatalf("panic %v does not mention %q", r, c.want)
				}
			}()
			registry.IsolateForTest(t)
			registry.RegisterBehavior("lightbox-probe", "(()=>{})()", c.opts...)
		})
	}
}

func stringifyPanic(r any) string {
	type stringer interface{ String() string }
	if s, ok := r.(stringer); ok {
		return s.String()
	}
	if s, ok := r.(string); ok {
		return s
	}
	return ""
}
