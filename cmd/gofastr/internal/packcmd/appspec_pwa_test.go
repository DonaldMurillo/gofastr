package packcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// pwaTestAppSpec loads a minimal app spec with the given app-block
// extras (indented two spaces, e.g. the pwa/llm_md lines).
func pwaTestAppSpec(t *testing.T, appExtras string) appspec.AppSpec {
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
	bp, err := appspec.LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	return bp
}

func pwaRenderedMain(t *testing.T, bp appspec.AppSpec) (string, []appgen.GeneratedFile) {
	t.Helper()
	files, err := appgen.RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	for _, f := range files {
		if f.Name == "main.go" {
			return f.Content, files
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

func TestAppSpecDecodesPWA(t *testing.T) {
	bp := pwaTestAppSpec(t, pwaBlockYAML+"  llm_md: true\n")
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

func TestAppSpecPWAEmitsWithPWA(t *testing.T) {
	bp := pwaTestAppSpec(t, pwaBlockYAML)
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
		if _, ok := wantIcons[f.Name]; ok {
			wantIcons[f.Name] = true
			if !strings.HasPrefix(f.Content, "\x89PNG\r\n\x1a\n") {
				t.Errorf("%s is not a PNG", f.Name)
			}
		}
	}
	for name, seen := range wantIcons {
		if !seen {
			t.Errorf("missing scaffolded icon %s", name)
		}
	}
}

// TestAppSpecPWACustomPrefixesDenied: a custom api_prefix (and a
// custom auth base_path) must flow into WithPWA's DenyPaths so the
// service worker's never-precache/never-intercept guarantee follows
// the app's real mounts, not the /api and /auth defaults.
func TestAppSpecPWACustomPrefixesDenied(t *testing.T) {
	bp := pwaTestAppSpec(t, "  api_prefix: v1\n"+pwaBlockYAML+`  auth:
    enabled: true
    base_path: /account
`)
	mainGo, _ := pwaRenderedMain(t, bp)
	if !strings.Contains(mainGo, `DenyPaths: []string{"/v1", "/account"}`) {
		t.Errorf("custom api_prefix + auth base_path should be emitted as DenyPaths:\n%s", mainGo)
	}

	// Default mounts need no extension: /api and /auth are built in.
	plain := pwaTestAppSpec(t, pwaBlockYAML)
	plainMain, _ := pwaRenderedMain(t, plain)
	if strings.Contains(plainMain, "DenyPaths") {
		t.Errorf("default prefixes must not emit DenyPaths:\n%s", plainMain)
	}
}

func TestAppSpecLLMMDEmitsOption(t *testing.T) {
	bp := pwaTestAppSpec(t, "  llm_md: true\n")
	mainGo, files := pwaRenderedMain(t, bp)
	if !strings.Contains(mainGo, "uihost.WithPublicLLMMD()") {
		t.Errorf("main.go should emit WithPublicLLMMD:\n%s", mainGo)
	}
	if strings.Contains(mainGo, "WithPWA") {
		t.Errorf("llm_md alone must not enable pwa:\n%s", mainGo)
	}
	for _, f := range files {
		if strings.Contains(f.Name, "icons/") {
			t.Errorf("llm_md alone must not scaffold icons: %s", f.Name)
		}
	}
}

func TestAppSpecPWALLMMatrix(t *testing.T) {
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
			mainGo, _ := pwaRenderedMain(t, pwaTestAppSpec(t, tc.extras))
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
	bp := pwaTestAppSpec(t, pwaBlockYAML+"  llm_md: true\n")
	files, err := appgen.RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("renderAppSpecFiles: %v", err)
	}
	dir := t.TempDir()
	for _, f := range files {
		full := filepath.Join(dir, f.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
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
	bp := pwaTestAppSpec(t, pwaBlockYAML+"  llm_md: true\n")
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

	// Absent features must not add keys (byte-compat for existing app specs).
	plain := pwaTestAppSpec(t, "")
	pm := appToMap(plain.App)
	if _, has := pm["pwa"]; has {
		t.Errorf("no pwa key expected when disabled")
	}
	if _, has := pm["llm_md"]; has {
		t.Errorf("no llm_md key expected when disabled")
	}
}
