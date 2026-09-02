package mcp

import (
	"context"
	"testing"
)

// Property: RegisterApp stamps the app linkage into a COPY of the
// caller's ToolMeta. A developer may share one meta map across several
// RegisterApp calls; stamping resourceUri in place would bleed the first
// app's linkage into every later app's tool (and into the caller's own
// map), pointing the model at the wrong resource — the exact bleed the
// clone comment in RegisterApp describes.
func TestRegisterAppDoesNotMutateCallerMeta(t *testing.T) {
	s := NewServer()
	sharedUI := map[string]any{"preferredSize": "large"}
	shared := map[string]any{"ui": sharedUI}

	appOne := sampleApp()
	appOne.Name = "app_one"
	appOne.ResourceURI = "ui://app/one.html"
	appOne.ToolMeta = shared
	if err := s.RegisterApp(appOne); err != nil {
		t.Fatal(err)
	}

	appTwo := sampleApp()
	appTwo.Name = "app_two"
	appTwo.ResourceURI = "ui://app/two.html"
	appTwo.ToolMeta = shared
	if err := s.RegisterApp(appTwo); err != nil {
		t.Fatal(err)
	}

	// The caller's maps carry no linkage stamps.
	if _, ok := sharedUI["resourceUri"]; ok {
		t.Error("RegisterApp wrote resourceUri into the caller's shared ui map")
	}
	if _, ok := shared["openai/outputTemplate"]; ok {
		t.Error("RegisterApp wrote openai/outputTemplate into the caller's shared meta map")
	}

	// Each app's tool links to its OWN resource, not the first app's.
	want := map[string]string{"app_one": "ui://app/one.html", "app_two": "ui://app/two.html"}
	lm := wireResult(t, s.HandleRequest(context.Background(), Request{JSONRPC: "2.0", ID: 1, Method: "tools/list"}))
	for _, raw := range lm["tools"].([]any) {
		tool := raw.(map[string]any)
		name := tool["name"].(string)
		meta := tool["_meta"].(map[string]any)
		if got := meta["ui"].(map[string]any)["resourceUri"]; got != want[name] {
			t.Errorf("SECURITY: [linkage] tool %q points at %v, want %v (meta bleed between apps)", name, got, want[name])
		}
		if got := meta["openai/outputTemplate"]; got != want[name] {
			t.Errorf("tool %q outputTemplate = %v, want %v", name, got, want[name])
		}
		// The caller's shared keys survive untouched.
		if meta["ui"].(map[string]any)["preferredSize"] != "large" {
			t.Errorf("tool %q lost the caller's ui.preferredSize: %v", name, meta["ui"])
		}
	}
}
