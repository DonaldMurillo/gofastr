package widget_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

// TestRuntimeTagEmbedsModuleManifest is the contract for kiln-style hosts
// that consume widget.RuntimeTag() instead of going through framework/uihost.
//
// Without the manifest in the page, the client-side loader falls back to an
// un-versioned URL (/__gofastr/runtime/<name>.js with no ?v=) which is then
// returned with `Cache-Control: ...immutable` — a poison-cache combo. Every
// kiln deploy that changes a split module would leave users on year-stale
// JS forever.
//
// The fix is to have RuntimeTag emit both the runtime <script> AND the
// `gofastr-runtime-modules` JSON manifest, so the loader can find the
// content-addressed hashes regardless of which framework rendered the page.
func TestRuntimeTagEmbedsModuleManifest(t *testing.T) {
	tag := widget.RuntimeTag()
	if !strings.Contains(tag, `<script src="/__gofastr/runtime.js?v=`) {
		t.Fatalf("RuntimeTag missing runtime.js script: %q", tag)
	}
	if !strings.Contains(tag, `gofastr-runtime-modules`) {
		t.Fatalf("RuntimeTag missing module manifest — kiln pages will poison-cache split modules.\nGot: %q", tag)
	}
	if !strings.Contains(tag, `type="application/json"`) {
		t.Fatalf("manifest must be an inert JSON script: %q", tag)
	}
	// Every embedded module must appear in the manifest with a non-empty
	// hash, otherwise loadModule constructs ?v= URLs without busting.
	for _, name := range runtime.ModuleNames() {
		if !strings.Contains(tag, `"`+name+`"`) {
			t.Errorf("manifest missing module %q", name)
		}
		hash := widget.RuntimeModuleHash(name)
		if hash == "" {
			t.Errorf("RuntimeModuleHash(%q) is empty", name)
			continue
		}
		if !strings.Contains(tag, hash) {
			t.Errorf("manifest missing hash %q for module %q", hash, name)
		}
	}
}
