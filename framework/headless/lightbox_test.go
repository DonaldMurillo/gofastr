package headless

import (
	"strings"
	"testing"
)

// The viewer's own contract. The universal sweeps (harness_test.go)
// cover every spec; these are the promises specific to a lightbox
// viewer, the way a11y_test.go holds the other components' specifics.

// The accessible name is what a screen reader is given when the modal
// takes focus, and it is reachable: the surrounding widget's
// aria-labelledby points at the title span's id, which is derived from
// the same Name the hooks carry.
func TestLightboxViewerTitleIsTheLabelledByTarget(t *testing.T) {
	got := LightboxViewer(LightboxViewerProps{Name: "photos"}, nil)
	has(t, got, `id="photos-title"`, "the title span's id is what the modal's aria-labelledby points at")
	has(t, got, ">Image viewer</span>", "the English default is the accessible name")
}

// A caller's Label wins over the string table's default, the same
// override every labelled component offers.
func TestLightboxViewerLabelOverrideWins(t *testing.T) {
	got := LightboxViewer(LightboxViewerProps{Name: "x", Label: "Slide 3 of 12"}, nil)
	has(t, got, ">Slide 3 of 12</span>", "the caller's label is the accessible name")
}

// The three rendered bindings are the widget contract's signal names:
// a viewer that renamed them would render a widget whose deeplink
// params never land. alt names the image for AT (text mode), src swaps
// the image (attr mode into src) and the download's href (attr mode),
// caption is prose (text mode). No html mode anywhere: every one of
// these values is URL-borne.
func TestLightboxViewerBindsTheWidgetContractSignals(t *testing.T) {
	got := LightboxViewer(LightboxViewerProps{Name: "x", Caption: true, Download: true}, nil)
	has(t, got, `data-fui-signal="alt"`, "the title span carries the alt signal")
	has(t, got, `data-fui-signal-attr="src"`, "the image's src is signal-written")
	has(t, got, `data-fui-signal="caption"`, "the caption carries the caption signal")
	has(t, got, `data-fui-signal-attr="href"`, "the download's href is signal-written")
	hasNot(t, got, `data-fui-signal-mode="html"`, "a URL-seeded value would render as markup in html mode")
}

// The zoom target is named by attribute, never by a class: a class map
// may rename every class, and the pinch-zoom module (framework/ui's)
// binds attributes only.
func TestLightboxViewerNamesTheZoomTargetByAttribute(t *testing.T) {
	bare := LightboxViewer(LightboxViewerProps{Name: "x"}, nil)
	has(t, bare, `data-hui-lightbox-image=""`, "the image carries the hook")
	hasNot(t, bare, `data-fui-lightbox-image`, "no Wiring means none of the framework binder's attributes")
	wired := LightboxViewer(LightboxViewerProps{Name: "x", Wiring: LightboxWiring{Viewer: "x"}}, nil)
	has(t, wired, `data-fui-lightbox-image=""`, "the Wiring renders the framework module's zoom target")
	hasNot(t, wired, `data-hui-lightbox-image`, "the wired render carries no hui twin for it — one binder per vocabulary")
}

// The two vocabularies are mutually exclusive, in both directions: a
// host's own viewer module (zero Wiring) is the only intended reader
// of the hui hooks, and the framework's module is the only reader of
// the data-fui-lightbox* wiring. A render carrying both invites the
// double-bind the suppression exists to prevent — a host module
// stepping the same gallery the shipped module steps.
func TestLightboxViewerWiringSuppressesTheHostHooks(t *testing.T) {
	wired := string(LightboxViewer(LightboxViewerProps{
		Name: "x", Nav: true, Wiring: LightboxWiring{Viewer: "x", Nav: true},
	}, nil))
	for _, twin := range []string{"data-hui-lightbox=", "data-hui-lightbox-nav", "data-hui-lightbox-image", "data-hui-lightbox-prev", "data-hui-lightbox-next"} {
		if strings.Contains(wired, twin) {
			t.Errorf("wired render carries %s — the hui family renders exactly when Wiring is zero:\n%s", twin, wired)
		}
	}
	bare := string(LightboxViewer(LightboxViewerProps{Name: "x", Nav: true}, nil))
	if strings.Contains(bare, "data-fui-lightbox") {
		t.Errorf("unwired render carries the framework binder's attributes — Wiring is what asks for them:\n%s", bare)
	}
}

// Nav without a viewer name is half-wired: the framework attributes
// would render an opt-in with nothing to opt into. Refused at render.
func TestLightboxViewerWiringNavNeedsAViewer(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Wiring.Nav without Wiring.Viewer should panic")
		}
	}()
	LightboxViewer(LightboxViewerProps{Name: "x", Wiring: LightboxWiring{Nav: true}}, nil)
}

// The nav buttons are named — an icon-only button with no name cannot
// be operated by anyone who cannot see it — and the download anchor
// keeps its plain href-less shape for the signal to fill.
func TestLightboxViewerControlsAreNamed(t *testing.T) {
	got := LightboxViewer(LightboxViewerProps{Name: "x", Nav: true, Download: true}, nil)
	has(t, got, `aria-label="Previous image"`, "the prev button is named")
	has(t, got, `aria-label="Next image"`, "the next button is named")
	has(t, got, `aria-label="Download image"`, "the download anchor is named")
	has(t, got, `type="button"`, "the nav buttons never submit a surrounding form")
}

// A partial translation falls back per field: a Strings with only the
// label translated still says the English default for the buttons.
func TestLightboxViewerPartialStringsFallBack(t *testing.T) {
	got := LightboxViewer(LightboxViewerProps{Name: "x", Nav: true,
		Strings: &Strings{LightboxViewerLabel: "Visionneuse"}}, nil)
	has(t, got, ">Visionneuse</span>", "the translated label is used")
	has(t, got, `aria-label="Previous image"`, "the untranslated button falls back to English")
}

// The nil render carries no class at all — the anatomy is structure.
// (The universal sweep asserts this for every case; this pins it for
// an ad-hoc render too, which no fixture sees.)
func TestLightboxViewerNilClassesCarriesNoClass(t *testing.T) {
	got := LightboxViewer(LightboxViewerProps{Name: "x", Nav: true}, nil)
	hasNot(t, got, "class=", "structure is carrying styling")
}

// The headless hooks are inert in this package: they render on the
// unwired path, where a host's own viewer module is the intended
// binder, and the wired path renders the data-fui-lightbox* family
// for framework/ui's module instead. The admission list
// (behavior_test.go hostHooks) carries each with its reason; this
// test fails if a hook is added without one, because hostHooks is
// checked against the spec's Hooks both ways.
func TestLightboxViewerHooksAreTheAdmittedSet(t *testing.T) {
	sp, ok := SpecOf("LightboxViewer")
	if !ok {
		t.Fatal("LightboxViewer has no spec")
	}
	want := []string{
		"data-hui-lightbox",
		"data-hui-lightbox-nav",
		"data-hui-lightbox-image",
		"data-hui-lightbox-prev",
		"data-hui-lightbox-next",
	}
	if strings.Join(sp.Hooks, ",") != strings.Join(want, ",") {
		t.Errorf("hooks = %v, want %v — a change here is a change to what a host's viewer module can bind, and to the hostHooks admission list", sp.Hooks, want)
	}
}
