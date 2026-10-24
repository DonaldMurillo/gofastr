package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAppSpecLocalePrefixesOption(t *testing.T) {
	mainGo, _ := pwaRenderedMain(t, pwaTestAppSpec(t, "  locale_prefixes: true\n"))
	if !strings.Contains(mainGo, "framework.WithLocalePrefixes(),") {
		t.Errorf("main.go should emit WithLocalePrefixes:\n%s", mainGo)
	}
	plain, _ := pwaRenderedMain(t, pwaTestAppSpec(t, ""))
	if strings.Contains(plain, "WithLocalePrefixes") {
		t.Errorf("WithLocalePrefixes without app.locale_prefixes:\n%s", plain)
	}
}

// A marketing footer under locale_prefixes carries the language
// switcher; without the key it does not.
func TestFooterLanguageMenuFollowsPrefixes(t *testing.T) {
	appGo := func(spec string) string {
		path := filepath.Join(t.TempDir(), "gofastr.yml")
		writeTestFile(t, path, spec)
		bp, err := loadAppSpec(path)
		if err != nil {
			t.Fatalf("loadAppSpec: %v", err)
		}
		return filesByName(mustRenderAppSpecFiles(t, bp))["app.go"]
	}
	const menu = "Language: ui.LanguageMenu(ui.LanguageMenuConfig{Ctx: ctx, Position: ui.MenuTopStart}),"
	prefixed := strings.Replace(chromeAppSpecYAML(), "  module: github.com/example/chroma\n",
		"  module: github.com/example/chroma\n  locale_prefixes: true\n", 1)
	if got := appGo(prefixed); !strings.Contains(got, menu) {
		t.Errorf("footer lacks the language menu under locale_prefixes:\n%s", got)
	}
	if got := appGo(chromeAppSpecYAML()); strings.Contains(got, "LanguageMenu") {
		t.Errorf("language menu without locale_prefixes:\n%s", got)
	}
}

// The locales README tells the reader how a locale is picked, and the
// header no longer picks it once the path does.
func TestLocalesReadmeFollowsPrefixes(t *testing.T) {
	readme := func(extra string) string {
		_, files := pwaRenderedMain(t, pwaTestAppSpec(t, extra))
		for _, f := range files {
			if f.name == "locales/README.md" {
				return f.content
			}
		}
		t.Fatal("no locales/README.md")
		return ""
	}
	if got := readme("  locale_prefixes: true\n"); !strings.Contains(got, "`/es/` serves es.json") || strings.Contains(got, "Accept-Language") {
		t.Errorf("README under locale_prefixes:\n%s", got)
	}
	if got := readme(""); !strings.Contains(got, "Accept-Language picks the locale") || strings.Contains(got, "{{pick}}") {
		t.Errorf("README without locale_prefixes:\n%s", got)
	}
}
