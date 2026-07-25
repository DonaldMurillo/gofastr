package pluginhost

import (
	"strings"
	"testing"
)

func TestManifestValidateRejectsAllowSameOrigin(t *testing.T) {
	m := Manifest{
		Entry:   "/p/editor.html",
		Sandbox: []string{"allow-scripts", "allow-same-origin"},
	}
	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "allow-same-origin") {
		t.Fatalf("expected allow-same-origin rejection, got %v", err)
	}
}

func TestManifestValidateRequiresAllowScriptsWhenSandboxSpecified(t *testing.T) {
	// An empty sandbox is normalised by the broker; but a non-empty sandbox
	// without allow-scripts could never boot its JS — reject it.
	m := Manifest{
		Entry:   "/p/editor.html",
		Sandbox: []string{},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("empty sandbox should be valid (broker normalises), got %v", err)
	}

	m.Sandbox = []string{"allow-forms"} // no allow-scripts
	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "allow-scripts") {
		t.Fatalf("expected allow-scripts requirement, got %v", err)
	}
}

func TestManifestValidateEntryRequired(t *testing.T) {
	m := Manifest{Sandbox: []string{"allow-scripts"}}
	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "entry") {
		t.Fatalf("expected entry-required error, got %v", err)
	}
}

func TestManifestValidateRejectsUnknownIsolation(t *testing.T) {
	m := Manifest{
		Entry:     "/p/editor.html",
		Isolation: "same-origin-component",
	}
	if err := m.Validate(); err == nil ||
		!strings.Contains(err.Error(), "unsupported isolation") {
		t.Fatalf("expected unsupported-isolation error, got %v", err)
	}
}

func TestManifestValidateAcceptsV1Fixpoint(t *testing.T) {
	m := Manifest{
		Entry:     "/p/editor.html",
		Isolation: IsolationSandboxOpaque,
		Sandbox:   []string{"allow-scripts"},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("v1 fixpoint should validate, got %v", err)
	}
	// Empty isolation defaults to the fixpoint (validated as acceptable).
	m.Isolation = ""
	if err := m.Validate(); err != nil {
		t.Fatalf("empty isolation should default-accept, got %v", err)
	}
}

func TestManifestSandboxString(t *testing.T) {
	cases := []struct {
		sandbox []string
		want    string
	}{
		{nil, "allow-scripts"},
		{[]string{}, "allow-scripts"},
		{[]string{"allow-scripts"}, "allow-scripts"},
		{[]string{"allow-scripts", "allow-popups"}, "allow-scripts allow-popups"},
	}
	for _, c := range cases {
		m := Manifest{Sandbox: c.sandbox}
		if got := m.SandboxString(); got != c.want {
			t.Errorf("SandboxString(%v)=%q want %q", c.sandbox, got, c.want)
		}
	}
}

// SandboxString is AUTHORITATIVE: even a manifest that carries
// allow-same-origin (bypassing Validate) must never emit it, and must always
// include allow-scripts.
func TestManifestSandboxString_StripsSameOriginForcesScripts(t *testing.T) {
	cases := []struct {
		sandbox []string
		want    string
	}{
		{[]string{"allow-scripts", "allow-same-origin"}, "allow-scripts"},
		{[]string{"allow-same-origin"}, "allow-scripts"},                              // forced in
		{[]string{"allow-popups", "allow-same-origin"}, "allow-scripts allow-popups"}, // stripped + forced
		{[]string{"allow-scripts", "allow-scripts"}, "allow-scripts"},                 // deduped
		{[]string{" allow-scripts ", "allow-same-origin "}, "allow-scripts"},          // trimmed + stripped
		{[]string{"allow-forms", "allow-same-origin", "allow-forms"}, "allow-scripts allow-forms"},
		// Round-4 bypasses: case variants + embedded whitespace must NOT slip
		// an effective allow-same-origin through (the attribute is
		// case-insensitive and whitespace-tokenised by the browser).
		{[]string{"Allow-Same-Origin"}, "allow-scripts"},
		{[]string{"ALLOW-SAME-ORIGIN"}, "allow-scripts"},
		// `x` is not a real sandbox token and is dropped along with
		// allow-same-origin: the filter is an allow-list now, so an
		// unrecognised token cannot ride through. See
		// TestSandboxDropsEscapeTokens for why.
		{[]string{"x allow-same-origin"}, "allow-scripts"},
		{[]string{"allow-scripts allow-same-origin"}, "allow-scripts"},
		{[]string{"allow-popups ALLOW-SAME-ORIGIN allow-forms"}, "allow-scripts allow-popups allow-forms"},
	}
	for _, c := range cases {
		m := Manifest{Sandbox: c.sandbox}
		got := m.SandboxString()
		if got != c.want {
			t.Errorf("SandboxString(%v)=%q want %q", c.sandbox, got, c.want)
		}
		if strings.Contains(got, "allow-same-origin") {
			t.Errorf("SandboxString(%v) leaked allow-same-origin: %q", c.sandbox, got)
		}
		if !strings.Contains(got, "allow-scripts") {
			t.Errorf("SandboxString(%v) missing allow-scripts: %q", c.sandbox, got)
		}
	}
}

func TestNewClientModule_ValidatesManifest(t *testing.T) {
	// Bad manifest (allow-same-origin) is rejected at construction.
	_, err := NewClientModule("p", Manifest{Entry: "/e.html", Sandbox: []string{"allow-scripts", "allow-same-origin"}}, nil)
	if err == nil {
		t.Fatal("NewClientModule must reject a manifest with allow-same-origin")
	}
	// Missing name rejected.
	if _, err := NewClientModule("", Manifest{Entry: "/e.html"}, nil); err == nil {
		t.Fatal("NewClientModule must reject an empty name")
	}
	// Valid manifest builds.
	cm, err := NewClientModule("p", Manifest{Entry: "/e.html", Sandbox: []string{"allow-scripts"}}, nil)
	if err != nil || cm.Name != "p" {
		t.Fatalf("valid module: %v / %+v", err, cm)
	}
}

// The JS sink (sandboxFor in host/pluginhost.js) is the one that actually sets
// the iframe attribute — it MUST be authoritative too, mirroring SandboxString.
// This pins the source so a regression to `manifest.sandbox.join(" ")` is
// caught without a browser.
func TestBrokerJS_SandboxForIsAuthoritative(t *testing.T) {
	js := string(brokerJSBytes)
	if strings.Contains(js, `manifest.sandbox.join(" ")`) {
		t.Error("sandboxFor must not join manifest.sandbox verbatim — it must strip allow-same-origin and force allow-scripts")
	}
	if !strings.Contains(js, "SAME_ORIGIN_COLLAPSING") {
		t.Error("sandboxFor must reference the same-origin-collapsing token filter")
	}
	if !strings.Contains(js, `unshift("allow-scripts")`) {
		t.Error("sandboxFor must force-include allow-scripts")
	}
	// It must normalize case + embedded whitespace before filtering (the
	// round-4 bypass), mirroring Go's strings.Fields(strings.ToLower(...)).
	if !strings.Contains(js, `.toLowerCase().split(/\s+/)`) {
		t.Error("sandboxFor must lowercase + whitespace-split each token before filtering (case/whitespace bypass)")
	}
}

// Manifest.Validate must also catch case/whitespace-smuggled allow-same-origin.
func TestManifestValidate_CaseAndWhitespaceSameOrigin(t *testing.T) {
	for _, sb := range [][]string{
		{"allow-scripts", "Allow-Same-Origin"},
		{"allow-scripts", "ALLOW-SAME-ORIGIN"},
		{"allow-scripts x allow-same-origin"},
	} {
		m := Manifest{Entry: "/e.html", Sandbox: sb}
		if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "allow-same-origin") {
			t.Errorf("Validate(%v) must reject smuggled allow-same-origin, got %v", sb, err)
		}
	}
}

// TestSandboxDropsEscapeTokens pins that the sandbox filter is an
// allow-list.
//
// SandboxString's own doc comment promises that "a mis-configured or
// tampered manifest cannot produce a de-opaqued frame". It could: the
// filter stripped exactly one token, allow-same-origin, and the manifest
// ships WITH the third-party plugin, so it is attacker-influenced by
// construction. `allow-popups` + `allow-popups-to-escape-sandbox` gives
// the plugin a popup that is fully unsandboxed AND same-origin —
// window.open('/admin/...') opens an ordinary cookie-bearing document,
// which is the isolation invariant gone. allow-top-navigation and
// allow-downloads passed too.
//
// The case shapes are the distinct escape mechanisms, not token
// variants: escape-via-popup, escape-via-navigation, escape-via-download,
// the direct same-origin grant, and a case/whitespace-obfuscated form of
// the same.
func TestSandboxDropsEscapeTokens(t *testing.T) {
	cases := []struct {
		name    string
		sandbox []string
		banned  string
	}{
		{"popup escape", []string{"allow-scripts", "allow-popups", "allow-popups-to-escape-sandbox"}, "allow-popups-to-escape-sandbox"},
		{"top navigation", []string{"allow-scripts", "allow-top-navigation"}, "allow-top-navigation"},
		{"top navigation by user", []string{"allow-scripts", "allow-top-navigation-by-user-activation"}, "allow-top-navigation-by-user-activation"},
		{"downloads", []string{"allow-scripts", "allow-downloads"}, "allow-downloads"},
		{"case + whitespace obfuscated", []string{"  Allow-Popups-To-Escape-Sandbox   allow-scripts "}, "allow-popups-to-escape-sandbox"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Manifest{Sandbox: c.sandbox}.SandboxString()
			if strings.Contains(got, c.banned) {
				t.Errorf("SECURITY: [sandbox-escape] SandboxString kept %q — the doc comment promises a tampered manifest cannot de-opaque the frame. Got: %q", c.banned, got)
			}
			if !strings.Contains(got, "allow-scripts") {
				t.Errorf("allow-scripts must survive — a plugin frame without it is inert. Got: %q", got)
			}
		})
	}

	// The capabilities a plugin legitimately needs must still pass.
	got := Manifest{Sandbox: []string{
		"allow-scripts", "allow-forms", "allow-modals",
		"allow-pointer-lock", "allow-orientation-lock", "allow-presentation", "allow-popups",
	}}.SandboxString()
	for _, want := range []string{"allow-forms", "allow-modals", "allow-pointer-lock", "allow-popups"} {
		if !strings.Contains(got, want) {
			t.Errorf("allow-list dropped the legitimate capability %q: %q", want, got)
		}
	}
}
