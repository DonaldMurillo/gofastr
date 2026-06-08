package framework

import "testing"

// fakeTypedPlugin is a plugin exposing an extra typed method so a caller
// can retrieve it as a concrete interface via PluginGetAs.
type fakeTypedPlugin struct{ name string }

func (p *fakeTypedPlugin) Name() string     { return p.name }
func (p *fakeTypedPlugin) Init(*App) error  { return nil }
func (p *fakeTypedPlugin) Greeting() string { return "hi from " + p.name }

type greeter interface{ Greeting() string }

// TestPluginGetAs_RoundTrips asserts typed retrieval succeeds and a
// wrong-type assertion returns an error rather than a zero value the
// caller might use unchecked.
func TestPluginGetAs_RoundTrips(t *testing.T) {
	pm := NewPluginManager()
	if err := pm.Register(&fakeTypedPlugin{name: "hello"}); err != nil {
		t.Fatalf("register: %v", err)
	}

	g, err := PluginGetAs[greeter](pm, "hello")
	if err != nil {
		t.Fatalf("PluginGetAs: %v", err)
	}
	if got := g.Greeting(); got != "hi from hello" {
		t.Fatalf("Greeting: got %q", got)
	}

	// Wrong type → error, not a usable zero value.
	if _, err := PluginGetAs[interface{ Nope() }](pm, "hello"); err == nil {
		t.Fatal("expected error for wrong type, got nil")
	}

	// Missing plugin → error.
	if _, err := PluginGetAs[greeter](pm, "absent"); err == nil {
		t.Fatal("expected error for missing plugin, got nil")
	}
}
