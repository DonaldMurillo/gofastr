package desktopcmd

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseLaunchArgs(t *testing.T) {
	got, err := parseLaunchArgs("desktop  --profile=work")
	if err != nil || len(got) != 2 || got[0] != "desktop" || got[1] != "--profile=work" {
		t.Fatalf("parseLaunchArgs = %q, %v", got, err)
	}
	for _, bad := range []string{" ", "desk\x00top", "a\x1bb", strings.Repeat("a ", 17), strings.Repeat("x", 257), "\xff"} {
		if _, err := parseLaunchArgs(bad); err == nil {
			t.Errorf("parseLaunchArgs(%q) accepted", bad)
		}
	}
}

func TestExtraLinkerFlags(t *testing.T) {
	extra, err := extraLinkerFlags(desktopBuildFlags{launchArgs: "desktop", ldflags: "-X main.version=1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(extra, "-X "+builtLaunchArgsVar+"=") || !strings.HasSuffix(extra, " -X main.version=1.2.3") {
		t.Fatalf("extraLinkerFlags = %q", extra)
	}
	if _, err := extraLinkerFlags(desktopBuildFlags{ldflags: "-X a=b\n-H x"}); err == nil {
		t.Fatal("a control byte in --ldflags was accepted")
	}
	if extra, _ := extraLinkerFlags(desktopBuildFlags{}); extra != "" {
		t.Fatalf("no flags gave %q", extra)
	}
}

// The build compiles battery/desktop's launch-args code.
//
//gofastr:test-reads battery/desktop/

// The stamp's variable name is a string in this package and a var in
// battery/desktop; build a real binary with it and run it, so a rename
// on either side fails here instead of shipping a no-op flag.
func TestLaunchArgsStampReachesBattery(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	// A scheme with no builtDeepLinkScheme stamp is the macOS build's
	// shape: only extraLinkerFlags carries it.
	extra, err := extraLinkerFlags(desktopBuildFlags{launchArgs: "desktop --profile=work", ldflags: "-s", scheme: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "launchmain")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-ldflags", strings.TrimSpace(extra), "-o", bin, "./testdata/launchmain")
	build.Env = append(build.Environ(), "CGO_ENABLED=0")
	build.WaitDelay = 10 * time.Second
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	run := exec.CommandContext(ctx, bin)
	run.WaitDelay = 10 * time.Second
	out, err := run.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `["desktop","--profile=work"]` {
		t.Fatalf("double-click args = %s; want the baked ones", got)
	}
	run = exec.CommandContext(ctx, bin, "serve")
	run.WaitDelay = 10 * time.Second
	out, err = run.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `["serve"]` {
		t.Fatalf("explicit args = %s; want them unchanged", got)
	}
	run = exec.CommandContext(ctx, bin, "notes://n/1")
	run.WaitDelay = 10 * time.Second
	out, err = run.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `["desktop","--profile=work","notes://n/1"]` {
		t.Fatalf("deep-link launch = %s; want the baked args in front of the link", got)
	}
}
