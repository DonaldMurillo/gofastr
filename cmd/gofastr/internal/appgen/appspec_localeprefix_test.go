package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A marketing footer under locale_prefixes carries the language
// switcher; without the key it does not.
func TestFooterLanguageMenuFollowsPrefixes(t *testing.T) {
	appGo := func(spec string) string {
		path := filepath.Join(t.TempDir(), "gofastr.yml")
		clitest.WriteTestFile(t, path, spec)
		bp, err := appspec.LoadAppSpec(path)
		if err != nil {
			t.Fatalf("loadAppSpec: %v", err)
		}
		return filesByName(mustRenderAppSpecFiles(t, bp))["app.go"]
	}
	const menu = "Language: ui.LanguageMenu(ui.LanguageMenuConfig{Ctx: ctx, Position: ui.MenuTopStart}),"
	prefixed := strings.Replace(clitest.ChromeAppSpecYAML(), "  module: github.com/example/chroma\n",
		"  module: github.com/example/chroma\n  locale_prefixes: true\n", 1)
	if got := appGo(prefixed); !strings.Contains(got, menu) {
		t.Errorf("footer lacks the language menu under locale_prefixes:\n%s", got)
	}
	if got := appGo(clitest.ChromeAppSpecYAML()); strings.Contains(got, "LanguageMenu") {
		t.Errorf("language menu without locale_prefixes:\n%s", got)
	}
}
