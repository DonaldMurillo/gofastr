package registry

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func mustPanic(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected a panic mentioning %q", want)
		}
		if !strings.Contains(r.(string), want) {
			t.Fatalf("panic %q does not mention %q", r, want)
		}
	}()
	fn()
}

// A behaviour registers under a name that is a URL segment and a
// manifest key, with at least one marker that is an attribute selector
// on a data- attribute. Every rule is a panic with its reason.
func TestRegisterBehaviorRules(t *testing.T) {
	IsolateForTest(t)
	mustPanic(t, "must match", func() { RegisterBehavior("Bad Name", "x", Markers("[data-x]")) })
	mustPanic(t, "must match", func() { RegisterBehavior("", "x", Markers("[data-x]")) })
	mustPanic(t, "must match", func() { RegisterBehavior("../evil", "x", Markers("[data-x]")) })
	mustPanic(t, "source is empty", func() { RegisterBehavior("empty", "  \n", Markers("[data-x]")) })
	mustPanic(t, "no Markers", func() { RegisterBehavior("nomarker", "x") })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("cls", "x", Markers(".a-class")) })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("role", "x", Markers(`[role="tree"]`)) })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("bare", "x", Markers("data-x")) })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("nested", "x", Markers(`[data-x="a]b"]`)) })
	// A value querySelector would throw on: an unescaped newline, a
	// backslash, DEL. One throw in the kernel's scan aborts the boot
	// pass for every module, so these are startup failures.
	mustPanic(t, "attribute selector", func() { RegisterBehavior("newline", "x", Markers("[data-x=\"a\nb\"]")) })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("backslash", "x", Markers(`[data-x="a\"]`)) })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("del", "x", Markers("[data-x=\"a\x7fb\"]")) })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("quote", "x", Markers(`[data-x="a"b"]`)) })
	mustPanic(t, "attribute selector", func() { RegisterBehavior("tab", "x", Markers("[data-x=\"a\tb\"]")) })
	RegisterBehavior("spaced-ok", "x", Markers(`[data-x="a b"]`)) // a space is fine
	// The name is a URL segment and a manifest key, at most 64 bytes:
	// the same bound compute.ValidName holds every module name to.
	RegisterBehavior("a"+strings.Repeat("b", 63), "x", Markers("[data-x]"))
	mustPanic(t, "must match", func() { RegisterBehavior("a"+strings.Repeat("b", 64), "x", Markers("[data-x]")) })
	b := RegisterBehavior("good", "(()=>{})()", Markers("[data-x]", `[data-y="v"]`), LoadIdle())
	if b.Name() != "good" || !b.Entry().Idle || len(b.Entry().Markers) != 2 {
		t.Fatalf("entry not as registered: %+v", b.Entry())
	}
	if b.Entry().SourceHash() == "" || len(b.Entry().SourceHash()) != 16 {
		t.Fatalf("source hash %q", b.Entry().SourceHash())
	}
}

// Identical re-registration is a no-op, as RegisterStyle's is; a
// different definition under the same name panics so a misname
// surfaces at startup.
func TestRegisterBehaviorDuplicates(t *testing.T) {
	IsolateForTest(t)
	a := RegisterBehavior("dup", "(()=>{})()", Markers("[data-x]"))
	b := RegisterBehavior("dup", "(()=>{})()", Markers("[data-x]"))
	if a.Entry() != b.Entry() {
		t.Fatal("identical re-registration did not return the existing entry")
	}
	mustPanic(t, "duplicate name", func() { RegisterBehavior("dup", "(()=>{ /* other */ })()", Markers("[data-x]")) })
	mustPanic(t, "duplicate name", func() { RegisterBehavior("dup", "(()=>{})()", Markers("[data-z]")) })
	mustPanic(t, "duplicate name", func() { RegisterBehavior("dup", "(()=>{})()", Markers("[data-x]"), LoadIdle()) })
}

// A reserved name (an embedded runtime module's) is refused where it
// is written.
func TestRegisterBehaviorRefusesReservedNames(t *testing.T) {
	IsolateForTest(t)
	ReserveBehaviorNames("copy")
	mustPanic(t, "embedded runtime module", func() { RegisterBehavior("copy", "x", Markers("[data-x]")) })
}

// A behaviour registered before the reservation arrives (its package
// initialised first) is refused when the reservation does arrive,
// still at init.
func TestReservationRefusesAnEarlierRegistration(t *testing.T) {
	IsolateForTest(t)
	RegisterBehavior("early", "x", Markers("[data-x]"))
	mustPanic(t, "already registered", func() { ReserveBehaviorNames("other", "early") })
}

// Behaviors is sorted, Lookup finds by name, and isolation hides
// what was registered before and drops what was registered inside.
func TestBehaviorsListingAndIsolation(t *testing.T) {
	IsolateForTest(t)
	RegisterBehavior("zeta", "x", Markers("[data-z]"))
	RegisterBehavior("alpha", "x", Markers("[data-a]"))
	names := []string{}
	for _, e := range Behaviors() {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "alpha,zeta" {
		t.Fatalf("order %v", names)
	}
	if _, ok := LookupBehavior("alpha"); !ok {
		t.Fatal("lookup missed alpha")
	}
	func() {
		IsolateForTest(t)
		if len(Behaviors()) != 0 {
			t.Fatal("nested isolation sees outer registrations")
		}
	}()
}

// A style and a behaviour may share a name: a component registers both
// under its own.
func TestStyleAndBehaviorMayShareAName(t *testing.T) {
	IsolateForTest(t)
	RegisterStyle("shared", func(style.Theme) string { return ".x{}" })
	RegisterBehavior("shared", "x", Markers("[data-shared]"))
	if _, ok := Lookup("shared"); !ok {
		t.Fatal("style lost")
	}
	if _, ok := LookupBehavior("shared"); !ok {
		t.Fatal("behaviour lost")
	}
}

// MarkerSubstring is what the host looks for in rendered HTML.
func TestMarkerSubstring(t *testing.T) {
	for in, want := range map[string]string{
		"[data-x]":       "data-x",
		`[data-x="v w"]`: `data-x="v w"`,
		`[data-x="a&b"]`: `data-x="a&amp;b"`, // as the renderer writes it
		`[data-x="<v>"]`: `data-x="&lt;v&gt;"`,
		".class":         "",
		"data-x":         "",
	} {
		if got := MarkerSubstring(in); got != want {
			t.Errorf("MarkerSubstring(%q) = %q, want %q", in, got, want)
		}
	}
}

// Requires records the modules a behaviour needs before it, in the
// order named and deduplicated, reachable from the entry Lookup and
// Behaviors hand out. A requirement that is not a module name, and a
// requirement on itself, are panics with their reasons.
func TestRegisterBehaviorRequires(t *testing.T) {
	IsolateForTest(t)
	mustPanic(t, "must match", func() { RegisterBehavior("bad-req", "x", Markers("[data-x]"), Requires("../evil")) })
	mustPanic(t, "must match", func() { RegisterBehavior("bad-req", "x", Markers("[data-x]"), Requires("Bad Name")) })
	mustPanic(t, "cannot require itself", func() { RegisterBehavior("selfish", "x", Markers("[data-x]"), Requires("selfish")) })
	b := RegisterBehavior("needs", "x", Markers("[data-x]"), Requires("action", "action", "other"))
	if strings.Join(b.Entry().Requires, ",") != "action,other" {
		t.Fatalf("Requires = %v, want [action other] deduplicated", b.Entry().Requires)
	}
	if e, ok := LookupBehavior("needs"); !ok || len(e.Requires) != 2 {
		t.Fatalf("LookupBehavior lost the requirements: %v %v", e, ok)
	}
	// A different requirement list is a different definition.
	mustPanic(t, "duplicate name", func() { RegisterBehavior("needs", "x", Markers("[data-x]"), Requires("action")) })
	// The same one re-registers as a no-op.
	if again := RegisterBehavior("needs", "x", Markers("[data-x]"), Requires("action", "other")); again.Entry() != b.Entry() {
		t.Fatal("identical re-registration with the same requirements did not return the existing entry")
	}
}

// Interactions record the retention the kernel's bridge performs,
// validated like markers: every refusal is a panic naming the field.
// The grammar is wider than a marker's — combinators, :not([attr]),
// comma lists — because the browser's querySelector is the only
// consumer; the lightbox's real scope (with :not([hidden])) is the
// widest in-tree shape and must register.
func TestRegisterBehaviorInteractions(t *testing.T) {
	IsolateForTest(t)
	// The event must be one the bridge branches on.
	mustPanic(t, "Event", func() {
		RegisterBehavior("ia-ev", "x", Markers("[data-x]"), Interactions(Interaction{Event: "submit"}))
	})
	mustPanic(t, "Event", func() {
		RegisterBehavior("ia-ev2", "x", Markers("[data-x]"), Interactions(Interaction{Event: ""}))
	})
	// A click needs its node selector, and only that.
	mustPanic(t, "Selector is empty", func() {
		RegisterBehavior("ia-click", "x", Markers("[data-x]"), Interactions(Interaction{Event: "click"}))
	})
	mustPanic(t, "Keys is set", func() {
		RegisterBehavior("ia-click2", "x", Markers("[data-x]"), Interactions(Interaction{Event: "click", Selector: "[data-x-go]", Keys: []string{"Enter"}}))
	})
	mustPanic(t, "Scope is set", func() {
		RegisterBehavior("ia-click3", "x", Markers("[data-x]"), Interactions(Interaction{Event: "click", Selector: "[data-x-go]", Scope: "[data-x]"}))
	})
	// A keydown needs keys and a scope, and no node selector: the
	// kernel resolves querySelector(scope) on every keydown and the
	// empty selector throws, so a scopeless spec is a listener that
	// dies on its first event.
	mustPanic(t, "Keys is empty", func() {
		RegisterBehavior("ia-key", "x", Markers("[data-x]"), Interactions(Interaction{Event: "keydown", Scope: "[data-x-open]"}))
	})
	mustPanic(t, "empty key", func() {
		RegisterBehavior("ia-key2", "x", Markers("[data-x]"), Interactions(Interaction{Event: "keydown", Keys: []string{"Enter", ""}, Scope: "[data-x-open]"}))
	})
	mustPanic(t, "Scope is empty", func() {
		RegisterBehavior("ia-key3", "x", Markers("[data-x]"), Interactions(Interaction{Event: "keydown", Keys: []string{"Enter"}}))
	})
	mustPanic(t, "Selector is set", func() {
		RegisterBehavior("ia-key4", "x", Markers("[data-x]"), Interactions(Interaction{Event: "keydown", Keys: []string{"Enter"}, Scope: "[data-x-open]", Selector: "[data-x-go]"}))
	})
	// The selector grammar: attribute selectors, :not([attr]),
	// combinators and comma lists pass; anything querySelector would
	// throw on, and the selector kinds no descriptor needs, refuse.
	for _, bad := range []string{
		"a[href]",             // type selector
		".nav",                // class selector
		"#go",                 // id selector
		"[data-x='v']",        // single-quoted value
		"[data-x=\"a\nb\"]",   // control character (newline) in value
		"[data-x=\"a\x7fb\"]", // DEL in value
		`[data-x="a\"]`,       // backslash in value
		"[data-x",             // unbalanced bracket
		":not([hidden]",       // unbalanced paren
		":not(.nav)",          // :not of a non-attribute
		":hover",              // pseudo-class
		"[data-x] >",          // trailing combinator
		"[data-x],[data-y",    // unbalanced list
	} {
		mustPanic(t, "selector list of attribute selectors", func() {
			RegisterBehavior("ia-sel", "x", Markers("[data-x]"), Interactions(Interaction{Event: "click", Selector: bad}))
		})
		IsolateForTest(t)
		mustPanic(t, "selector list of attribute selectors", func() {
			RegisterBehavior("ia-scope", "x", Markers("[data-x]"), Interactions(Interaction{Event: "keydown", Keys: []string{"Enter"}, Scope: bad}))
		})
		IsolateForTest(t)
	}
	// CSS whitespace is all five characters, not just space and tab:
	// a selector written across two source lines is browser-valid and
	// must register. The quoted value still refuses every control
	// character, which the bad list above pins.
	for _, ws := range []string{"\n", "\r", "\f", "\r\n", " \n\t"} {
		IsolateForTest(t)
		RegisterBehavior("ia-ws", "x", Markers("[data-x]"), Interactions(
			Interaction{Event: "click", Selector: "[data-x-a]" + ws + "[data-x-b]"},
			Interaction{Event: "click", Selector: "[data-x-a]" + ws + ">" + ws + "[data-x-b]"},
			Interaction{Event: "click", Selector: "[data-x-a]," + ws + "[data-x-b]"},
		))
	}
	IsolateForTest(t)
	// The entry stops being the caller's at registration: a later write
	// to the slice that was passed in must not reach a descriptor that
	// has already been validated and is on its way to the manifest.
	keys := []string{"ArrowLeft", "ArrowRight"}
	kb := RegisterBehavior("ia-keys", "x", Markers("[data-x]"), Interactions(
		Interaction{Event: "keydown", Keys: keys, Scope: "[data-x-open]"},
	))
	keys[0] = "Escape"
	if got := kb.Entry().Interactions[0].Keys[0]; got != "ArrowLeft" {
		t.Fatalf("the caller's later write reached the registered entry: Keys[0] = %q", got)
	}
	IsolateForTest(t)
	// The widest in-tree shapes register: the lightbox's real pair.
	b := RegisterBehavior("ia-real", "x", Markers("[data-x]"), Interactions(
		Interaction{Event: "click", Selector: "[data-x-prev],[data-x-next]"},
		Interaction{Event: "keydown", Keys: []string{"ArrowLeft", "ArrowRight"}, Scope: `[data-x-widget]:not([hidden]) [data-x-comp="viewer"][data-x]`},
		Interaction{Event: "click", Selector: "[data-x-widget] > [data-x-go]"},
	))
	ia := b.Entry().Interactions
	if len(ia) != 3 || ia[1].Keys[0] != "ArrowLeft" || ia[2].Selector != "[data-x-widget] > [data-x-go]" {
		t.Fatalf("interactions not as registered: %+v", ia)
	}
	if e, ok := LookupBehavior("ia-real"); !ok || len(e.Interactions) != 3 {
		t.Fatalf("LookupBehavior lost the interactions: %v %v", e, ok)
	}
	// A different interaction list is a different definition.
	mustPanic(t, "duplicate name", func() {
		RegisterBehavior("ia-real", "x", Markers("[data-x]"), Interactions(Interaction{Event: "click", Selector: "[data-x-other]"}))
	})
	// The same list, re-registered, is a no-op.
	if again := RegisterBehavior("ia-real", "x", Markers("[data-x]"), Interactions(
		Interaction{Event: "click", Selector: "[data-x-prev],[data-x-next]"},
		Interaction{Event: "keydown", Keys: []string{"ArrowLeft", "ArrowRight"}, Scope: `[data-x-widget]:not([hidden]) [data-x-comp="viewer"][data-x]`},
		Interaction{Event: "click", Selector: "[data-x-widget] > [data-x-go]"},
	)); again.Entry() != b.Entry() {
		t.Fatal("identical re-registration with the same interactions did not return the existing entry")
	}
}
