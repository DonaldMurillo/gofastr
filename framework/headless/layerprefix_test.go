package headless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
)

// The headless layer names no kit class and no data-fui-* attribute:
// its modules bind data-hui-* hooks and dress the rows they build from
// the template the kit registers. The two deliberate exceptions, the
// Safe refusal list and the lightbox Wiring, carry the allow marker with
// their reason. Live-tree gate over this package's Go and JavaScript,
// with a vacuity control.
func TestHeadlessNamesNoKitVocabulary(t *testing.T) {
	js, err := filepath.Glob("*.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(js) == 0 {
		t.Fatal("no behaviour module source beside this package")
	}
	jsres, err := check.LintLayerPrefixJS(js...)
	if err != nil {
		t.Fatal(err)
	}
	if jsres.HasErrors() {
		t.Errorf("a headless module names the kit's vocabulary:\n%s", strings.TrimSpace(jsres.Error()))
	}
	gores, err := check.LintLayerPrefixGo(".")
	if err != nil {
		t.Fatal(err)
	}
	if gores.HasErrors() {
		t.Errorf("headless Go names the kit's vocabulary:\n%s", strings.TrimSpace(gores.Error()))
	}

	vdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vdir, "vacuity.js"),
		[]byte("(function(){ const w = document.createElement('div'); w.className = 'fui-notification fui-notification--' + v; })();\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintLayerPrefixJS(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintLayerPrefixJS no longer fires on the v0.86 toast builder spelling")
	}
}
