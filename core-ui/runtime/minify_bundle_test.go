package runtime

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

// TestMinifiedBundle runs the minifier's corpus and security checks on
// the composed full bundle, which only this package can compose: the
// output shrinks, is a fixed point, keeps its anchors and the URL-scheme
// guard, and gains no </script>.
func TestMinifiedBundle(t *testing.T) {
	src := composedRuntime(t)
	out := minify.Minify(src)
	if out == "" || len(out) >= len(src) {
		t.Fatalf("minify did not shrink the bundle (in=%d out=%d)", len(src), len(out))
	}
	if again := minify.Minify(out); again != out {
		t.Errorf("minify is not idempotent on the bundle (len1=%d len2=%d)", len(out), len(again))
	}
	for _, want := range []string{"data-cui-os", "__gofastr", "_isUnsafeSignalUrl", "'javascript:'", "'vbscript:'", "'data:'"} {
		if !strings.Contains(out, want) {
			t.Errorf("SECURITY: minified bundle lost %q", want)
		}
	}
	if in, got := strings.Count(strings.ToLower(src), "</script>"), strings.Count(strings.ToLower(out), "</script>"); got > in {
		t.Errorf("SECURITY: [script-closer] minify introduced </script> (in=%d out=%d)", in, got)
	}
}

// composedRuntime is the full composition RuntimeJS minifies, unminified:
// the source the gate tests read.
func composedRuntime(t testing.TB) string {
	t.Helper()
	src, err := composeFull()
	if err != nil {
		t.Fatalf("composeFull: %v", err)
	}
	return src
}

// TestComputeMarkerMatchesRuntimeJS: the composed bundle's demand-load
// scanner names the compute module and its data-cui-compute marker.
func TestComputeMarkerMatchesRuntimeJS(t *testing.T) {
	src := composedRuntime(t)
	if !strings.Contains(src, `{ name: 'compute',`) ||
		!strings.Contains(src, `selector: '[data-cui-compute]'`) {
		t.Fatal("runtime scanner missing data-cui-compute marker")
	}
}
