package main

// Gates for the vendored gofastr-plugins registry and the pages rendered
// from it. The copy is consumed by copy, not import, so nothing else
// notices when it goes stale or malformed; these tests do.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPluginRegistryVendoredCopy(t *testing.T) {
	reg, err := parsePluginRegistry(pluginRegistryJSON)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Release.Commit == "" || reg.Release.Published == "" || reg.Release.Source == "" {
		t.Errorf("release stamp incomplete: %+v", *reg.Release)
	}
	if !strings.HasPrefix(reg.Release.Source, "https://github.com/") {
		t.Errorf("release source %q is not a GitHub URL", reg.Release.Source)
	}
	seen := map[string]bool{}
	root := ""
	for _, p := range reg.Plugins {
		if seen[p.Name] {
			t.Errorf("%s: duplicate row", p.Name)
		}
		seen[p.Name] = true
		for k, v := range map[string]string{"modulePath": p.ModulePath, "version": p.Version, "description": p.Description, "isolation": p.Isolation, "frameworkCompat": p.FrameworkCompat, "routePrefix": p.RoutePrefix} {
			if v == "" {
				t.Errorf("%s: empty %s", p.Name, k)
			}
		}
		if root == "" {
			root = pluginRootModule(p.ModulePath)
		} else if got := pluginRootModule(p.ModulePath); got != root {
			t.Errorf("%s: root module %q, want %q", p.Name, got, root)
		}
		switch p.Isolation {
		case "sandbox-iframe-opaque":
			if p.Trusted {
				t.Errorf("%s: sandboxed row marked trusted", p.Name)
			}
			for _, tok := range p.Sandbox {
				if tok == "allow-same-origin" {
					t.Errorf("%s: sandbox grants allow-same-origin, which collapses the opaque origin", p.Name)
				}
			}
		case "trusted-host-page":
			if !p.Trusted {
				t.Errorf("%s: trusted-host-page row without trusted:true", p.Name)
			}
			if len(p.Sandbox) != 0 {
				t.Errorf("%s: trusted row declares sandbox tokens %v", p.Name, p.Sandbox)
			}
		default:
			t.Errorf("%s: unknown isolation %q", p.Name, p.Isolation)
		}
	}
}

func TestPluginRegistryRejectsUnstampedOrUnknown(t *testing.T) {
	var raw map[string]any
	if err := json.Unmarshal(pluginRegistryJSON, &raw); err != nil {
		t.Fatal(err)
	}
	delete(raw, "release")
	unstamped, _ := json.Marshal(raw)
	if _, err := parsePluginRegistry(unstamped); err == nil {
		t.Error("a copy with no release stamp parsed; a hand copy from git would pass as published")
	}
	raw["release"] = map[string]string{"tag": "v0.0.0", "commit": "x", "published": "now", "source": "https://github.com/x/y"}
	raw["newKey"] = true
	unknown, _ := json.Marshal(raw)
	if _, err := parsePluginRegistry(unknown); err == nil {
		t.Error("a copy with an unknown top-level key parsed; a new registry field would be silently dropped")
	}
}

func TestPluginsIndexListsEveryPlugin(t *testing.T) {
	reg, err := pluginReg()
	if err != nil {
		t.Fatal(err)
	}
	page := body(t, "/plugins")
	if !strings.Contains(page, reg.Release.Tag) {
		t.Errorf("/plugins does not show the vendored release tag %s", reg.Release.Tag)
	}
	for _, p := range reg.Plugins {
		if !strings.Contains(page, `href="/plugins/`+p.Name+`"`) {
			t.Errorf("/plugins has no link to /plugins/%s", p.Name)
		}
	}
	if !strings.Contains(page, `href="/docs/plugin-platform"`) {
		t.Error("/plugins does not link the plugin platform doc")
	}
}

func TestPluginPagesShowMountSnippet(t *testing.T) {
	reg, err := pluginReg()
	if err != nil {
		t.Fatal(err)
	}
	paths := (&PluginScreen{}).StaticPaths(t.Context())
	if len(paths) != len(reg.Plugins) {
		t.Errorf("StaticPaths = %d, want one per plugin (%d)", len(paths), len(reg.Plugins))
	}
	for _, p := range reg.Plugins {
		page := body(t, "/plugins/"+p.Name)
		for _, want := range []string{p.ModulePath, "RegisterPlugin(", "go get " + pluginRootModule(p.ModulePath) + "@" + reg.Release.Tag, "pkg.go.dev/" + p.ModulePath} {
			if !strings.Contains(page, want) {
				t.Errorf("/plugins/%s missing %q", p.Name, want)
			}
		}
		if p.Docs != "" && !strings.Contains(page, "/blob/"+reg.Release.Tag+"/"+p.Docs) {
			t.Errorf("/plugins/%s docs link is not pinned to %s", p.Name, reg.Release.Tag)
		}
	}
	if rec := serve(t, http.MethodGet, "/plugins/not-a-plugin"); rec.Code != http.StatusNotFound {
		t.Errorf("/plugins/not-a-plugin: got %d, want 404", rec.Code)
	}
}

func TestPluginSummaryCutsAtFirstSentence(t *testing.T) {
	cases := map[string]string{
		"One sentence.": "One sentence.",
		"First sentence. Second sentence that goes on.": "First sentence.",
		strings.Repeat("word ", 50) + "end":             strings.TrimSpace(strings.Repeat("word ", 36)) + "…",
		"A v0.1.0 thing (see docs/x.md) with no period": "A v0.1.0 thing (see docs/x.md) with no period",
	}
	for in, want := range cases {
		if got := pluginSummary(in); got != want {
			t.Errorf("pluginSummary(%q) = %q, want %q", in, got, want)
		}
	}
}
