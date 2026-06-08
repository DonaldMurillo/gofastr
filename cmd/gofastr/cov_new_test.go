package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewUsagePrints(t *testing.T) {
	out := covT_capStdout(t, newUsage)
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("newUsage output: %s", out)
	}
}

func TestRunNewDispatchesAllResources(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)

	covT_capStdout(t, func() { runNew([]string{"handler", "Ping", "--method=POST", "--path=/p"}) })
	if _, err := os.Stat(filepath.Join(dir, "ping_handler.go")); err != nil {
		t.Fatalf("handler not created: %v", err)
	}

	out := covT_capStdout(t, func() { runNew([]string{"route", "/x", "--method=POST", "--handler=h"}) })
	if !strings.Contains(out, "app.Router().Handle") {
		t.Fatalf("route snippet missing: %s", out)
	}
}

func TestRunNewHelp(t *testing.T) {
	out := covT_capStdout(t, func() { runNew([]string{"--help"}) })
	if !strings.Contains(out, "Usage:") {
		t.Fatalf("help: %s", out)
	}
}

func TestRunNewNoArgsExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNew(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewUnknownResourceExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNew([]string{"frobnicate"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewEntityRemovedExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNew([]string{"entity", "Widget"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewHandlerNoNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewHandler(nil, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewHandlerInvalidNameExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewHandler([]string{"a/b"}, false) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}

func TestRunNewRouteNoArgsExits(t *testing.T) {
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() { runNewRoute(nil) })
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
