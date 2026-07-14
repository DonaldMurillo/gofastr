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
		{[]string{"x allow-same-origin"}, "allow-scripts x"},
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
