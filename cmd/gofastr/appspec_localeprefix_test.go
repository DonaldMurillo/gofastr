package main

import (
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
