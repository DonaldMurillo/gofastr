package native

import (
	"runtime"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
)

// TestShellMatchesPlatform pins the selection contract: Shell() hands
// back a native shell on darwin/arm64 and windows/amd64, and the
// unsupported shell naming GOOS/GOARCH elsewhere. SetTrayTitle remains
// unsupported on a native shell when the app has no configured tray.
func TestShellMatchesPlatform(t *testing.T) {
	s := Shell()
	err := s.SetTrayTitle("probe")
	de, ok := err.(*desktop.Error)
	if !ok || de.Code != desktop.CodeUnsupported {
		t.Fatalf("SetTrayTitle probe: %v, want an unsupported refusal", err)
	}
	hasNative := (runtime.GOOS == "darwin" && runtime.GOARCH == "arm64") ||
		(runtime.GOOS == "windows" && runtime.GOARCH == "amd64")
	namedPlatform := strings.Contains(de.Message, "GOOS="+runtime.GOOS)
	if hasNative == namedPlatform {
		t.Fatalf("GOOS=%s GOARCH=%s: Shell() answered %q; want a native shell on darwin/arm64 and windows/amd64, and the unsupported one elsewhere",
			runtime.GOOS, runtime.GOARCH, de.Message)
	}
}

// TestNewFillsShellAndKeepsExplicit: a nil cfg.Shell gets Shell(); an
// explicit Shell (the test double) is passed through untouched.
func TestNewFillsShellAndKeepsExplicit(t *testing.T) {
	b := New(desktop.Config{ID: "dev.gofastr.native-test"})
	if b.Shell() == nil {
		t.Fatal("New left Shell nil")
	}
	nilErr := b.Shell().SetTrayTitle("probe")
	wantErr := Shell().SetTrayTitle("probe")
	if nilErr.Error() != wantErr.Error() {
		t.Fatalf("New's default shell (%v) is not Shell() (%v)", nilErr, wantErr)
	}

	fake := desktoptest.NewShell()
	b2 := New(desktop.Config{ID: "dev.gofastr.native-test", Shell: fake})
	if b2.Shell() != fake {
		t.Fatalf("New overrode an explicit Shell: %T", b2.Shell())
	}
}
