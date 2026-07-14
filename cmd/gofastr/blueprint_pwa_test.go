package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pwaTestBlueprint loads a minimal blueprint with the given app-block
// extras (indented two spaces, e.g. the pwa/llm_md lines).
func pwaTestBlueprint(t *testing.T, appExtras string) Blueprint {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	yml := "app:\n  name: Demo\n  module: example.com/demo\n" + appExtras + `
screens:
  - name: home
    route: /
    title: Home
`
	if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	bp, err := loadBlueprint(path)
	if err != nil {
		t.Fatalf("loadBlueprint: %v", err)
	}
	return bp
}

func pwaRenderedMain(t *testing.T, bp Blueprint) (string, []generatedFile) {
	t.Helper()
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	for _, f := range files {
		if f.name == "main.go" {
			return f.content, files
		}
	}
	t.Fatalf("no main.go in rendered files")
	return "", nil
}

const pwaBlockYAML = `  pwa:
    enabled: true
    short_name: Dm
    description: Demo tracker
    theme_color: "#112233"
    background_color: "#ffffff"
    display: minimal-ui
`

func TestBlueprintDecodesPWA(t *testing.T) {
	bp := pwaTestBlueprint(t, pwaBlockYAML+"  llm_md: true\n")
	p := bp.App.PWA
	if !p.Enabled {
		t.Fatalf("pwa.enabled should decode, got %#v", p)
	}
	if p.ShortName != "Dm" || p.Description != "Demo tracker" {
		t.Errorf("short_name/description: %#v", p)
	}
	if p.ThemeColor != "#112233" || p.BackgroundColor != "#ffffff" {
		t.Errorf("colors: %#v", p)
	}
	if p.Display != "minimal-ui" {
		t.Errorf("display: %q", p.Display)
	}
	if !bp.App.LLMMD {
		t.Errorf("llm_md should decode true")
	}
}

func TestBlueprintRejectsUnknownPWAKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	writeTestFile(t, path, "app:\n  name: Demo\n  pwa:\n    enabled: true\n    bogus: 1\n")
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "bogus"`) {
		t.Fatalf("loadBlueprint err = %v", err)
	}
}

func TestBlueprintRejectsBadPWADisplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	writeTestFile(t, path, "app:\n  name: Demo\n  pwa:\n    enabled: true\n    display: popup\n")
	_, err := loadBlueprint(path)
	if err == nil || !strings.Contains(err.Error(), "app.pwa.display") {
		t.Fatalf("loadBlueprint err = %v", err)
	}
}

func TestBlueprintPWAEmitsWithPWA(t *testing.T) {
	bp := pwaTestBlueprint(t, pwaBlockYAML)
	mainGo, files := pwaRenderedMain(t, bp)
	if !strings.Contains(mainGo, "uihost.WithPWA(uihost.PWAConfig{") {
		t.Errorf("main.go should emit WithPWA:\n%s", mainGo)
	}
	if !strings.Contains(mainGo, "uihost.PWADisplayMinimalUI") {
		t.Errorf("display should map to the typed constant:\n%s", mainGo)
	}
	if !strings.Contains(mainGo, `"/icons/icon-192.png"`) ||
		!strings.Contains(mainGo, `"/icons/icon-512.png"`) ||
		!strings.Contains(mainGo, `"/icons/icon-maskable.png"`) {
		t.Errorf("WithPWA should declare the scaffolded icons:\n%s", mainGo)
	}
	if !strings.Contains(mainGo, "uihost.PWAIconPurposeMaskable") {
		t.Errorf("maskable icon should carry the typed purpose:\n%s", mainGo)
	}
	if strings.Contains(mainGo, "WithPublicLLMMD") {
		t.Errorf("pwa alone must not enable llm_md:\n%s", mainGo)
	}
	// Scaffolded icons: replaceable PNG files under the static dir.
	wantIcons := map[string]bool{
		"static/icons/icon-192.png":      false,
		"static/icons/icon-512.png":      false,
		"static/icons/icon-maskable.png": false,
	}
	for _, f := range files {
		if _, ok := wantIcons[f.name]; ok {
			wantIcons[f.name] = true
			if !strings.HasPrefix(f.content, "\x89PNG\r\n\x1a\n") {
				t.Errorf("%s is not a PNG", f.name)
			}
		}
	}
	for name, seen := range wantIcons {
		if !seen {
			t.Errorf("missing scaffolded icon %s", name)
		}
	}
}

// TestBlueprintPWACustomPrefixesDenied: a custom api_prefix (and a
// custom auth base_path) must flow into WithPWA's DenyPaths so the
// service worker's never-precache/never-intercept guarantee follows
// the app's real mounts, not the /api and /auth defaults.
func TestBlueprintPWACustomPrefixesDenied(t *testing.T) {
	bp := pwaTestBlueprint(t, "  api_prefix: v1\n"+pwaBlockYAML+`  auth:
    enabled: true
    base_path: /account
`)
	mainGo, _ := pwaRenderedMain(t, bp)
	if !strings.Contains(mainGo, `DenyPaths: []string{"/v1", "/account"}`) {
		t.Errorf("custom api_prefix + auth base_path should be emitted as DenyPaths:\n%s", mainGo)
	}

	// Default mounts need no extension — /api and /auth are built in.
	plain := pwaTestBlueprint(t, pwaBlockYAML)
	plainMain, _ := pwaRenderedMain(t, plain)
	if strings.Contains(plainMain, "DenyPaths") {
		t.Errorf("default prefixes must not emit DenyPaths:\n%s", plainMain)
	}
}

func TestBlueprintLLMMDEmitsOption(t *testing.T) {
	bp := pwaTestBlueprint(t, "  llm_md: true\n")
	mainGo, files := pwaRenderedMain(t, bp)
	if !strings.Contains(mainGo, "uihost.WithPublicLLMMD()") {
		t.Errorf("main.go should emit WithPublicLLMMD:\n%s", mainGo)
	}
	if strings.Contains(mainGo, "WithPWA") {
		t.Errorf("llm_md alone must not enable pwa:\n%s", mainGo)
	}
	for _, f := range files {
		if strings.Contains(f.name, "icons/") {
			t.Errorf("llm_md alone must not scaffold icons: %s", f.name)
		}
	}
}

func TestBlueprintPWALLMMatrix(t *testing.T) {
	cases := []struct {
		name       string
		extras     string
		wantPWA    bool
		wantLLMMMD bool
	}{
		{"neither", "", false, false},
		{"pwa-only", pwaBlockYAML, true, false},
		{"llm-only", "  llm_md: true\n", false, true},
		{"both", pwaBlockYAML + "  llm_md: true\n", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mainGo, _ := pwaRenderedMain(t, pwaTestBlueprint(t, tc.extras))
			if got := strings.Contains(mainGo, "uihost.WithPWA("); got != tc.wantPWA {
				t.Errorf("WithPWA emitted = %v, want %v", got, tc.wantPWA)
			}
			if got := strings.Contains(mainGo, "uihost.WithPublicLLMMD()"); got != tc.wantLLMMMD {
				t.Errorf("WithPublicLLMMD emitted = %v, want %v", got, tc.wantLLMMMD)
			}
		})
	}
}

func TestPackReadsPWAAndLLMMD(t *testing.T) {
	bp := pwaTestBlueprint(t, pwaBlockYAML+"  llm_md: true\n")
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("renderBlueprintFiles: %v", err)
	}
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := packReadApp(dir)
	if err != nil {
		t.Fatalf("packReadApp: %v", err)
	}
	if got.PWA != bp.App.PWA {
		t.Errorf("pack should round-trip the pwa block:\n got %#v\nwant %#v", got.PWA, bp.App.PWA)
	}
	if !got.LLMMD {
		t.Errorf("pack should recover llm_md: true")
	}
}

func TestAppToMapSerializesPWAAndLLMMD(t *testing.T) {
	bp := pwaTestBlueprint(t, pwaBlockYAML+"  llm_md: true\n")
	m := appToMap(bp.App)
	pwa, ok := m["pwa"].(map[string]any)
	if !ok {
		t.Fatalf("appToMap should emit a pwa block, got %#v", m)
	}
	if pwa["display"] != "minimal-ui" || pwa["theme_color"] != "#112233" {
		t.Errorf("pwa map: %#v", pwa)
	}
	if m["llm_md"] != true {
		t.Errorf("llm_md should serialize, got %#v", m["llm_md"])
	}

	// Absent features must not add keys (byte-compat for existing blueprints).
	plain := pwaTestBlueprint(t, "")
	pm := appToMap(plain.App)
	if _, has := pm["pwa"]; has {
		t.Errorf("no pwa key expected when disabled")
	}
	if _, has := pm["llm_md"]; has {
		t.Errorf("no llm_md key expected when disabled")
	}
}
