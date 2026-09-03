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
	if len(reg.Plugins) < 2 {
		t.Fatalf("vendored registry has %d plugins; the mutation cases below need at least two", len(reg.Plugins))
	}
}

// mutateRegistry decodes the vendored copy loosely, lets the case edit it,
// and re-encodes it, so every guard in parsePluginRegistry is exercised
// against a copy that is valid except for the one thing the case breaks.
func mutateRegistry(t *testing.T, edit func(raw map[string]any, rows []map[string]any)) []byte {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(pluginRegistryJSON, &raw); err != nil {
		t.Fatal(err)
	}
	rowsAny := raw["plugins"].([]any)
	rows := make([]map[string]any, len(rowsAny))
	for i, r := range rowsAny {
		rows[i] = r.(map[string]any)
	}
	edit(raw, rows)
	out, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPluginRegistryGuardsEachFail(t *testing.T) {
	sandboxed := func(rows []map[string]any) map[string]any {
		for _, r := range rows {
			if r["isolation"] == "sandbox-iframe-opaque" {
				return r
			}
		}
		t.Fatal("no sandboxed row in the vendored copy")
		return nil
	}
	trusted := func(rows []map[string]any) map[string]any {
		for _, r := range rows {
			if r["isolation"] == "trusted-host-page" {
				return r
			}
		}
		t.Fatal("no trusted row in the vendored copy")
		return nil
	}
	cases := map[string]func(raw map[string]any, rows []map[string]any){
		"no release stamp":             func(raw map[string]any, _ []map[string]any) { delete(raw, "release") },
		"release stamp without commit": func(raw map[string]any, _ []map[string]any) { delete(raw["release"].(map[string]any), "commit") },
		"unknown top-level key":        func(raw map[string]any, _ []map[string]any) { raw["newKey"] = true },
		"unknown row key":              func(_ map[string]any, rows []map[string]any) { rows[0]["newKey"] = true },
		"registry version 2":           func(raw map[string]any, _ []map[string]any) { raw["registryVersion"] = "2" },
		"no plugins":                   func(raw map[string]any, _ []map[string]any) { raw["plugins"] = []any{} },
		"duplicate name":               func(_ map[string]any, rows []map[string]any) { rows[1]["name"] = rows[0]["name"] },
		"empty version":                func(_ map[string]any, rows []map[string]any) { rows[0]["version"] = "" },
		"unknown isolation":            func(_ map[string]any, rows []map[string]any) { rows[0]["isolation"] = "same-origin-frame" },
		"sandboxed row marked trusted": func(_ map[string]any, rows []map[string]any) { sandboxed(rows)["trusted"] = true },
		"sandboxed row allows same-origin": func(_ map[string]any, rows []map[string]any) {
			r := sandboxed(rows)
			r["sandbox"] = append(toAnySlice(r["sandbox"]), "allow-same-origin")
		},
		"trusted row without trusted:true": func(_ map[string]any, rows []map[string]any) { delete(trusted(rows), "trusted") },
		"trusted row with sandbox tokens":  func(_ map[string]any, rows []map[string]any) { trusted(rows)["sandbox"] = []any{"allow-scripts"} },
		"row in a different module":        func(_ map[string]any, rows []map[string]any) { rows[1]["modulePath"] = "github.com/someone/else/x" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePluginRegistry(mutateRegistry(t, edit)); err == nil {
				t.Errorf("parsed a copy with %s; that guard is not guarding", name)
			}
		})
	}
	t.Run("trailing data", func(t *testing.T) {
		if _, err := parsePluginRegistry(append(append([]byte{}, pluginRegistryJSON...), []byte(" {}")...)); err == nil {
			t.Error("parsed a copy followed by a second JSON value")
		}
	})
}

func toAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	return v.([]any)
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
