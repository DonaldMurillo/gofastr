package desktop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Group 3: registry validation, freeze, and manifest determinism.

func regCap(name string) Capability {
	return Capability{
		Name:    name,
		Version: 1,
		Methods: []Method{{
			Name:    "do",
			Handler: func(ctx context.Context, in json.RawMessage) (any, error) { return nil, nil },
		}},
	}
}

func TestRegistryValidationRules(t *testing.T) {
	cases := []struct {
		name string
		cap  Capability
		want string // substring of the error
	}{
		{"bad capability name", Capability{Name: "Bad-Name", Version: 1, Methods: regCap("x").Methods}, "capability name"},
		{"leading digit capability name", Capability{Name: "9clip", Version: 1, Methods: regCap("x").Methods}, "capability name"},
		{"version zero", Capability{Name: "clip", Version: 0, Methods: regCap("x").Methods}, "version"},
		{"no methods", Capability{Name: "clip", Version: 1}, "at least one method"},
		{"bad method name", Capability{Name: "clip", Version: 1, Methods: []Method{{Name: "Do-Thing", Handler: regCap("x").Methods[0].Handler}}}, "method name"},
		{"method name starts uppercase", Capability{Name: "clip", Version: 1, Methods: []Method{{Name: "Do", Handler: regCap("x").Methods[0].Handler}}}, "method name"},
		{"duplicate method names", Capability{Name: "clip", Version: 1, Methods: []Method{
			{Name: "do", Handler: regCap("x").Methods[0].Handler},
			{Name: "do", Handler: regCap("x").Methods[0].Handler},
		}}, "duplicate method"},
		{"bad permission grammar", Capability{Name: "clip", Version: 1, Methods: []Method{{
			Name: "do", Permission: "clipboard", Handler: regCap("x").Methods[0].Handler,
		}}}, "permission"},
		{"permission with wildcard", Capability{Name: "clip", Version: 1, Methods: []Method{{
			Name: "do", Permission: "clip:*", Handler: regCap("x").Methods[0].Handler,
		}}}, "permission"},
		{"uppercase permission", Capability{Name: "clip", Version: 1, Methods: []Method{{
			Name: "do", Permission: "Clip:Write", Handler: regCap("x").Methods[0].Handler,
		}}}, "permission"},
		{"nil handler", Capability{Name: "clip", Version: 1, Methods: []Method{{Name: "do"}}}, "Handler"},
		{"invalid input schema", Capability{Name: "clip", Version: 1, Methods: []Method{{
			Name: "do", Input: json.RawMessage(`{no`), Handler: regCap("x").Methods[0].Handler,
		}}}, "Input"},
		{"invalid output schema", Capability{Name: "clip", Version: 1, Methods: []Method{{
			Name: "do", Output: json.RawMessage(`{no`), Handler: regCap("x").Methods[0].Handler,
		}}}, "Output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry()
			if err := r.register(tc.cap, "test"); err == nil {
				t.Fatal("expected an error")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err.Error(), tc.want)
			}
		})
	}
}

func TestValidPermissionGrammarAccepted(t *testing.T) {
	r := newRegistry()
	cap := regCap("clip")
	cap.Methods[0].Permission = "clipboard:write"
	if err := r.register(cap, "test"); err != nil {
		t.Fatalf("valid permission rejected: %v", err)
	}
}

func TestDuplicateNamesFirstRegistrantSite(t *testing.T) {
	b, _ := newTestBattery(t)
	if err := b.Register(Capability{
		Name:    "myplug",
		Version: 1,
		Methods: []Method{{Name: "go", Handler: func(ctx context.Context, in json.RawMessage) (any, error) { return nil, nil }}},
	}); err != nil {
		t.Fatal(err)
	}
	err := b.Register(Capability{
		Name:    "myplug",
		Version: 2,
		Methods: []Method{{Name: "other", Handler: func(ctx context.Context, in json.RawMessage) (any, error) { return nil, nil }}},
	})
	if err == nil {
		t.Fatal("duplicate accepted")
	}
	if !strings.Contains(err.Error(), "capability_test.go") {
		t.Fatalf("duplicate error must name the FIRST registration site, got: %v", err)
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("error wording: %v", err)
	}
}

func TestRegisterAfterFreezeFails(t *testing.T) {
	b, _ := newTestBattery(t)
	b.freezeForTest()
	err := b.Register(regCap("lateplug"))
	if err == nil || !strings.Contains(err.Error(), "froze") {
		t.Fatalf("register after freeze: %v", err)
	}
}

func TestManifestDeterministicAcrossRegistrationOrders(t *testing.T) {
	build := func(order []string) []byte {
		b, _ := newTestBattery(t)
		for _, name := range order {
			cap := regCap(name)
			if err := b.Register(cap); err != nil {
				t.Fatal(err)
			}
		}
		b.freezeForTest()
		m, err := b.Manifest()
		if err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a := build([]string{"alpha", "zeta", "mid"})
	b2 := build([]string{"zeta", "mid", "alpha"})
	if string(a) != string(b2) {
		t.Fatalf("manifest not deterministic:\n%s\n%s", a, b2)
	}
}

func TestManifestBeforeFreezeIsError(t *testing.T) {
	b, _ := newTestBattery(t)
	if _, err := b.Manifest(); err == nil {
		t.Fatal("Manifest before freeze must error")
	}
}

func TestManifestShape(t *testing.T) {
	b, _ := newTestBattery(t)
	b.freezeForTest()
	m, err := b.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != 1 {
		t.Fatalf("schema = %d", m.Schema)
	}
	if m.Host.OS == "" || m.Host.Arch == "" || m.Host.Version == "" {
		t.Fatalf("host info incomplete: %+v", m.Host)
	}
	// Sorted by name; the five core capabilities are present.
	names := make([]string, len(m.Capabilities))
	for i, c := range m.Capabilities {
		names[i] = c.Name
		for j := 1; j < len(c.Methods); j++ {
			if c.Methods[j-1].Name > c.Methods[j].Name {
				t.Fatalf("methods of %s not sorted", c.Name)
			}
		}
		for _, mm := range c.Methods {
			if mm.Permission != "" && !rePermission.MatchString(mm.Permission) {
				t.Fatalf("manifest carries invalid permission %q", mm.Permission)
			}
		}
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("capabilities not sorted: %v", names)
		}
	}
	want := map[string]bool{"window": true, "windows": true, "dialogs": true, "clipboard": true, "notifications": true, "fs": true, "tray": true}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for n := range want {
		if !got[n] {
			t.Fatalf("core capability %s missing from manifest", n)
		}
	}
}
