package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// A helper stub named register_generated emits func RegisterGenerated in
// stubs.go beside app.go's own. Each file parses alone, so the per-file
// check passed and the app failed to compile after a successful generate.
func TestHelperRedeclaringGeneratedFails(t *testing.T) {
	bp := hookBp(validHook)
	bp.Helpers = []appspec.AppSpecNamedStub{{Name: "register_generated"}}
	_, err := RenderAppSpecFiles(bp)
	if err == nil {
		t.Fatal("a helper that redeclares RegisterGenerated must fail the generate")
	}
	for _, want := range []string{"RegisterGenerated", "stubs.go", "app.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name %q: %v", want, err)
		}
	}
}

// A hook handler is emitted verbatim, so one named like a package the
// emitted package imports (http) shadows that import name: Go keeps
// package-level and file-scope import names in one space.
func TestHookShadowingImportFails(t *testing.T) {
	h := validHook
	h.Handler = "http"
	bp := hookBp(h)
	bp.Middleware = []appspec.AppSpecNamedStub{{Name: "audit"}} // stubs.go imports net/http
	_, err := RenderAppSpecFiles(bp)
	if err == nil || !strings.Contains(err.Error(), "imports as a package name") {
		t.Fatalf("a hook handler named like an import must fail the generate, got %v", err)
	}
}

func TestDistinctStubNamesRender(t *testing.T) {
	bp := hookBp(validHook)
	bp.Helpers = []appspec.AppSpecNamedStub{{Name: "format_price"}}
	bp.Middleware = []appspec.AppSpecNamedStub{{Name: "audit"}}
	if _, err := RenderAppSpecFiles(bp); err != nil {
		t.Fatalf("distinct stub names must render: %v", err)
	}
}
