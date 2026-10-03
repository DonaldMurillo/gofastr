package widget_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
)

// TestRuntimeTagEmbedsModuleManifest is the contract for kiln-style hosts
// that consume widget.RuntimeTag() instead of going through framework/uihost.
//
// Without the manifest in the page, the client-side loader falls back to an
// un-versioned URL (/__gofastr/runtime/<name>.js with no ?v=) which is then
// returned with `Cache-Control: ...immutable`, a poison-cache combo. Every
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
	// The kernel parses the inline blocks while runtime.js executes,
	// so every block RuntimeModuleManifestScript emits must precede
	// the script tag; a block that follows it is read as absent, and
	// for #gofastr-behaviors that means no registered behaviour's
	// marker is ever scanned on a kiln page. This package registers
	// no behaviour, so the module manifest, which every binary
	// carries, stands for the whole run of blocks; the behaviours
	// block is checked too when a host's binary emits it.
	si := strings.Index(tag, `<script src="/__gofastr/runtime.js`)
	if mi := strings.Index(tag, `id="gofastr-runtime-modules"`); mi < 0 || mi > si {
		t.Fatalf("the module manifest must precede runtime.js in RuntimeTag (block at %d, script at %d):\n%s", mi, si, tag)
	}
	if bi := strings.Index(tag, `id="gofastr-behaviors"`); bi > si {
		t.Fatalf("the behaviours block must precede runtime.js in RuntimeTag (block at %d, script at %d)", bi, si)
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

// The behaviours block carries registered markers to the browser as an
// inline JSON script; a marker value that spells a closing tag must not
// end the block early, and must round-trip.
func TestBehaviorsManifestEscapesClosingScript(t *testing.T) {
	registry.IsolateForTest(t)
	registry.RegisterBehavior("esc-probe", "(()=>{})()", registry.Markers(`[data-esc="</script><script>alert(1)</script>"]`))
	script := widget.BehaviorsManifestScript()
	if script == "" {
		t.Fatal("no block for a registered behaviour")
	}
	inner := script[strings.IndexByte(script, '>')+1 : strings.LastIndex(script, "</script>")]
	if strings.Contains(inner, "</") {
		t.Fatalf("block contains a raw closing tag: %q", inner)
	}
	var got map[string]struct {
		S []string `json:"s"`
	}
	if err := json.Unmarshal([]byte(inner), &got); err != nil {
		t.Fatalf("escaped block JSON: %v", err)
	}
	if len(got["esc-probe"].S) != 1 || got["esc-probe"].S[0] != `[data-esc="</script><script>alert(1)</script>"]` {
		t.Fatalf("marker did not round-trip: %v", got["esc-probe"].S)
	}
}
