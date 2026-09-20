package runtime

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

const probeJS = "(() => {\n  // a registered behaviour\n  window.__probe = 1;\n})();\n"

// A registered behaviour is a module from the host down: listed,
// served, hashed, and preloaded like an embedded one.
func TestRegisteredBehaviorIsAModule(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("probe-beh", probeJS, registry.Markers("[data-probe]", `[data-probe-kind="x"]`))

	names := ModuleNames()
	found := false
	for _, n := range names {
		if n == "probe-beh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ModuleNames() lacks the registered behaviour: %v", names)
	}
	src, ok := Module("probe-beh")
	if !ok || !strings.Contains(src, "__probe") {
		t.Fatalf("Module() = %q, %v", src, ok)
	}
	if nominify() {
		if src != probeJS {
			t.Fatalf("with minification off the source must be served as registered")
		}
	} else if strings.Contains(src, "a registered behaviour") {
		t.Fatalf("with minification on the comment must be gone: %q", src)
	}
	h := ModuleHash("probe-beh")
	if len(h) != 16 {
		t.Fatalf("ModuleHash = %q", h)
	}
	if ModuleHash("probe-beh") != h {
		t.Fatal("hash is not stable across calls")
	}
	if ModuleHash("no-such-module") != "" {
		t.Fatal("an unknown name has a hash")
	}
	// Embedded modules keep theirs.
	if ModuleHash("copy") == "" {
		t.Fatal("embedded module lost its hash")
	}
}

// The hash follows the source: a different registration under the same
// name (in a fresh isolation) serves and versions the new bytes.
func TestRegisteredBehaviorHashFollowsSource(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("probe-beh", probeJS, registry.Markers("[data-probe]"))
	h1 := ModuleHash("probe-beh")
	// A subtest, so its isolation is restored when it ends, before the
	// assertion below.
	t.Run("different source", func(t *testing.T) {
		registry.IsolateForTest(t)
		registry.RegisterBehavior("probe-beh", probeJS+"window.__probe2 = 2;\n", registry.Markers("[data-probe]"))
		if h2 := ModuleHash("probe-beh"); h2 == h1 {
			t.Fatal("a different source kept the old hash")
		}
		if src, _ := Module("probe-beh"); !strings.Contains(src, "__probe2") {
			t.Fatal("a different source was served from the stale cache")
		}
	})
	if h := ModuleHash("probe-beh"); h != h1 {
		t.Fatal("restoring the registration did not restore its hash")
	}
	if src, _ := Module("probe-beh"); strings.Contains(src, "__probe2") {
		t.Fatal("restoring the registration still serves the other source")
	}
}

// NeededModules preloads a registered behaviour when one of its
// markers appears in the page, at an attribute-name boundary, and
// never as the prefix of a longer attribute.
func TestNeededModulesMatchesRegisteredMarkers(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("probe-beh", probeJS, registry.Markers("[data-probe]", `[data-probe-kind="x"]`))
	has := func(page string) bool {
		for _, n := range NeededModules(page) {
			if n == "probe-beh" {
				return true
			}
		}
		return false
	}
	if !has(`<div data-probe="">`) || !has(`<div data-probe>`) || !has(`<div data-probe-kind="x">`) {
		t.Fatal("a present marker was not matched")
	}
	if has(`<div data-probe-other="">`) {
		t.Fatal("a longer attribute matched as the marker")
	}
	if has(`<div data-probe-kind="y">`) {
		t.Fatal("a valued marker matched a different value")
	}
	// A value the renderer escapes is matched in its rendered form.
	registry.RegisterBehavior("amp-beh", probeJS, registry.Markers(`[data-amp="a&b"]`))
	rendered := string(render.Tag("div", html.Attrs{"data-amp": "a&b"}))
	found := false
	for _, n := range NeededModules(rendered) {
		if n == "amp-beh" {
			found = true
		}
	}
	if !found {
		t.Fatalf("an escaped attribute value was not matched; rendered %q", rendered)
	}
	if has(`<div>`) {
		t.Fatal("matched with no marker present")
	}
}

// The behaviours block carries every registered behaviour's markers
// and idle flag, nothing when none is registered, and escapes the one
// sequence that could end an inline script.
func TestBehaviorsJSON(t *testing.T) {
	registry.IsolateForTest(t)
	if BehaviorsJSON() != nil {
		t.Fatal("an empty registry produced a block")
	}
	registry.RegisterBehavior("b-idle", probeJS, registry.Markers("[data-b]"), registry.LoadIdle())
	registry.RegisterBehavior("a-now", probeJS, registry.Markers("[data-a]", `[data-a-k="</script>"]`))
	var got map[string]struct {
		S []string `json:"s"`
		I bool     `json:"i"`
	}
	if err := json.Unmarshal(BehaviorsJSON(), &got); err != nil {
		t.Fatal(err)
	}
	if !got["b-idle"].I || got["a-now"].I {
		t.Fatalf("idle flags wrong: %+v", got)
	}
	if strings.Join(got["a-now"].S, "|") != `[data-a]|[data-a-k="</script>"]` {
		t.Fatalf("markers wrong: %v", got["a-now"].S)
	}
}

// mustPanicNames runs fn and fails when it does not panic with a
// message containing want.
func mustPanicNames(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("no panic, want one naming %q", want)
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, want) {
			t.Fatalf("panic %v does not name %q", r, want)
		}
	}()
	fn()
}

// A valid requirement rides the behaviours block as r, so the kernel
// loads the primitive before the behaviour that needs it. A
// requirement that is neither an embedded module nor a registered
// behaviour, and a cycle, panic at BehaviorsJSON time with the names:
// the first render that builds the manifest, not startup (the
// registry is only complete once every package's init has run) — see
// TestCyclePanicSurfacesAs500ThroughARealHost for what that panic
// does to a real request.
func TestBehaviorsJSONRequirements(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("dep", probeJS, registry.Markers("[data-dep]"))
	registry.RegisterBehavior("user", probeJS, registry.Markers("[data-user]"), registry.Requires("dep", "copy"))
	var got map[string]struct {
		R []string `json:"r"`
	}
	if err := json.Unmarshal(BehaviorsJSON(), &got); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got["user"].R, ",") != "dep,copy" {
		t.Fatalf("requirements = %v, want dep,copy", got["user"].R)
	}
	if len(got["dep"].R) != 0 {
		t.Fatalf("a behaviour with no requirements carried r: %v", got["dep"].R)
	}

	registry.IsolateForTest(t)
	registry.RegisterBehavior("user", probeJS, registry.Markers("[data-user]"), registry.Requires("no-such-module"))
	mustPanicNames(t, "no-such-module", func() { BehaviorsJSON() })

	registry.IsolateForTest(t)
	registry.RegisterBehavior("aa", probeJS, registry.Markers("[data-aa]"), registry.Requires("bb"))
	registry.RegisterBehavior("bb", probeJS, registry.Markers("[data-bb]"), registry.Requires("cc"))
	registry.RegisterBehavior("cc", probeJS, registry.Markers("[data-cc]"), registry.Requires("aa"))
	mustPanicNames(t, "aa -> bb -> cc -> aa", func() { BehaviorsJSON() })
}

// The interactions ride the behaviours block as x, in the kernel
// bridge's own spec shape, beside s/i/r; a behaviour with none adds
// no x key to the payload at all. BehaviorsJSON is the one function
// behind every delivery shape (manifest.js's
// window.__gofastr_behaviors, the export/embed inline block, the
// theme editor's head block), so the field reaching it here reaches
// each of them — the browser tests prove the kernel's read end-to-end.
func TestBehaviorsJSONInteractions(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("plain", probeJS, registry.Markers("[data-plain]"))
	registry.RegisterBehavior("interactive", probeJS, registry.Markers("[data-ia]"),
		registry.Interactions(
			registry.Interaction{Event: "click", Selector: "[data-ia-prev],[data-ia-next]"},
			registry.Interaction{Event: "keydown", Keys: []string{"ArrowLeft", "ArrowRight"}, Scope: `[data-ia-widget]:not([hidden]) [data-ia]`},
		))
	var got map[string]struct {
		X []struct {
			Event    string   `json:"event"`
			Selector string   `json:"selector"`
			Keys     []string `json:"keys"`
			Scope    string   `json:"scope"`
		} `json:"x"`
	}
	if err := json.Unmarshal(BehaviorsJSON(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got["plain"].X) != 0 {
		t.Fatalf("a behaviour with no interactions carried x: %v", got["plain"].X)
	}
	x := got["interactive"].X
	if len(x) != 2 {
		t.Fatalf("x = %v, want the two declared specs", x)
	}
	if x[0].Event != "click" || x[0].Selector != "[data-ia-prev],[data-ia-next]" || x[0].Keys != nil || x[0].Scope != "" {
		t.Fatalf("click spec wrong: %+v", x[0])
	}
	if x[1].Event != "keydown" || x[1].Selector != "" || strings.Join(x[1].Keys, ",") != "ArrowLeft,ArrowRight" || x[1].Scope != `[data-ia-widget]:not([hidden]) [data-ia]` {
		t.Fatalf("keydown spec wrong: %+v", x[1])
	}
	// The omitempty is the "costs nothing" half: a registry where
	// nothing declares interactions carries no x at all, so the
	// payload every page bears is unchanged for the behaviours that
	// need no retention.
	registry.IsolateForTest(t)
	registry.RegisterBehavior("plain", probeJS, registry.Markers("[data-plain]"))
	if buf := BehaviorsJSON(); strings.Contains(string(buf), `"x"`) {
		t.Fatalf("x rode the block with no behaviour declaring interactions: %s", buf)
	}
}

// A behaviour registered under an embedded module's name is refused
// twice: at registration, by the names this package reserved at init,
// and where the two sets meet, for a registration that ran before the
// reservation (a package that does not import this one can init
// first). Isolation clears the reservations, so the second path is
// reachable here; the first is re-armed by reserving the name.
func TestBehaviorShadowingAnEmbeddedModuleIsRefused(t *testing.T) {
	expectPanic := func(t *testing.T, fn func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil || !strings.Contains(r.(string), "embedded runtime module") {
				t.Fatalf("expected the shadow refusal, got %v", r)
			}
		}()
		fn()
	}
	t.Run("at registration", func(t *testing.T) {
		registry.IsolateForTest(t)
		registry.ReserveBehaviorNames("copy")
		expectPanic(t, func() { registry.RegisterBehavior("copy", probeJS, registry.Markers("[data-copy-probe]")) })
	})
	t.Run("where the sets meet", func(t *testing.T) {
		registry.IsolateForTest(t)
		registry.RegisterBehavior("copy", probeJS, registry.Markers("[data-copy-probe]"))
		expectPanic(t, func() { ModuleNames() })
		expectPanic(t, func() { BehaviorsJSON() })
	})
}
