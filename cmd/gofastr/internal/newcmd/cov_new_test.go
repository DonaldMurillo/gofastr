package newcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestNewUsagePrints(t *testing.T) {
	out := clitest.CovT_capStdout(t, newUsage)
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("newUsage output: %s", out)
	}
}

func TestRunNewDispatchesAllResources(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)

	clitest.CovT_capStdout(t, func() { RunNew([]string{"handler", "Ping", "--method=POST", "--path=/p"}) })
	if _, err := os.Stat(filepath.Join(dir, "ping_handler.go")); err != nil {
		t.Fatalf("handler not created: %v", err)
	}

	out := clitest.CovT_capStdout(t, func() { RunNew([]string{"route", "/x", "--method=POST", "--handler=h"}) })
	if !strings.Contains(out, "app.Router().Handle") {
		t.Fatalf("route snippet missing: %s", out)
	}
}

func TestRunNewHelp(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { RunNew([]string{"--help"}) })
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("help: %s", out)
	}
}

func TestRunNewNoArgsExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunNew(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewUnknownResourceExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunNew([]string{"frobnicate"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewEntityRemovedExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunNew([]string{"entity", "Widget"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewHandlerNoNameExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runNewHandler(nil, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewHandlerInvalidNameExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runNewHandler([]string{"a/b"}, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewRouteNoArgsExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { runNewRoute(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestExtractOverwriteFlag(t *testing.T) {
	ov, rest := extractOverwriteFlag([]string{"a", "-overwrite", "b"})
	if !ov || len(rest) != 2 || rest[0] != "a" || rest[1] != "b" {
		t.Fatalf("got ov=%v rest=%v", ov, rest)
	}
	ov2, rest2 := extractOverwriteFlag([]string{"x", "--overwrite"})
	if !ov2 || len(rest2) != 1 {
		t.Fatalf("got ov=%v rest=%v", ov2, rest2)
	}
	ov3, _ := extractOverwriteFlag([]string{"x"})
	if ov3 {
		t.Fatalf("expected no overwrite")
	}
}
