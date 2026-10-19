package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
)

// The kernel names no kit class and no data-fui-* attribute: its
// vocabulary is data-cui-*/cui-*, and the hooks it binds in headless
// markup are data-hui-* (formerrors.js). The rule lives in
// core-ui/check.LintLayerPrefix*; this is the live-tree gate for the
// runtime package's frag/ and src/, with a vacuity control so a quiet
// result means clean, not blind. core-ui/check's
// TestLintLayerPrefix_RepoIsClean holds the core-ui and headless Go
// trees to the same rule.
func TestKernelNamesNoKitVocabulary(t *testing.T) {
	js, err := check.LintLayerPrefixJS(".")
	if err != nil {
		t.Fatal(err)
	}
	if js.HasErrors() {
		t.Errorf("the runtime names the kit's vocabulary:\n%s", strings.TrimSpace(js.Error()))
	}

	vdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(vdir, "vacuity.js"),
		[]byte("(function(){ const p = document.createElement('p'); p.className = 'fui-field__error'; })();\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintLayerPrefixJS(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintLayerPrefixJS no longer fires on the v0.86 formerrors spelling")
	}
}
