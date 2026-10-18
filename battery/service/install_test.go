package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The service definition carries the CLI's state base, so a daemon a
// service manager starts (without the shell's environment) opens the
// same directory the CLI talks to.
func TestDaemonArgvCarriesBaseDir(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	args, err := b.daemonArgv("/opt/hello", 47811)
	if err != nil {
		t.Fatal(err)
	}
	want := "/opt/hello service daemon --port 47811 --base-dir " + os.Getenv(dataDirEnv)
	if got := strings.Join(args, " "); got != want {
		t.Fatalf("argv %q; want %q", got, want)
	}
	b.baseOverride = "/elsewhere"
	l, err := b.layoutFor(false)
	if err != nil {
		t.Fatal(err)
	}
	if l.dir != filepath.Join("/elsewhere", "dev.gofastr.svctest") {
		t.Fatalf("layout with --base-dir = %s", l.dir)
	}
}

func TestDaemonRefusesRelativeBaseDir(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	err := b.dispatch(context.Background(), []string{"daemon", "--base-dir", "rel/dir"}, nil)
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative --base-dir error = %v", err)
	}
}

// A state base whose socket path overruns sun_path is refused before
// anything is registered, not left as a crash-looping service.
func TestInstallRefusesLongSocketPathFirst(t *testing.T) {
	b, ft, _ := testBattery(t, "darwin", Config{})
	t.Setenv(dataDirEnv, filepath.Join(t.TempDir(), strings.Repeat("d", maxSocketPath)))
	err := b.install(b.cfg.Port)
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("install error = %v", err)
	}
	if len(ft.got()) != 0 {
		t.Fatalf("ran tools before refusing: %q", ft.got())
	}
}

func TestDevLayoutPerWorktree(t *testing.T) {
	b, _, _ := testBattery(t, "darwin", Config{})
	devIsolationID = func() string { return "wt_abc" }
	l, err := b.layoutFor(true)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(l.dir) != "dev.gofastr.svctest.dev-wt_abc" {
		t.Fatalf("dev dir %s", l.dir)
	}
	inst, _ := b.layoutFor(false)
	if filepath.Base(inst.dir) != "dev.gofastr.svctest" {
		t.Fatalf("installed dir %s carries the worktree id", inst.dir)
	}
}

// GOFASTR_ISOLATION_ID is free text; anything but a short plain word
// is hashed before it becomes a path segment.
func TestPathSegment(t *testing.T) {
	for _, id := range []string{"wt_plain-1", "a", strings.Repeat("x", 32)} {
		if got := pathSegment(id); got != id {
			t.Errorf("pathSegment(%q) = %q; want it unchanged", id, got)
		}
	}
	for _, id := range []string{"../../etc", "a b", `c:\x`, "", strings.Repeat("x", 33), "é"} {
		got := pathSegment(id)
		if len(got) != 10 || strings.Trim(got, "0123456789abcdef") != "" {
			t.Errorf("pathSegment(%q) = %q; want a 10-hex-digit hash", id, got)
		}
	}
}

func TestDefaultBrowser(t *testing.T) {
	cases := []struct {
		name  string
		store bool
		out   string
		want  string
	}{
		{"no store", false, "", "com.apple.Safari"},
		{"http handler", true, `[{"LSHandlerURLScheme":"mailto","LSHandlerRoleAll":"com.apple.mail"},{"LSHandlerURLScheme":"http","LSHandlerRoleAll":"com.microsoft.edgemac"}]`, "com.microsoft.edgemac"},
		{"no http entry", true, `[{"LSHandlerContentType":"public.html","LSHandlerRoleAll":"com.example.editor"}]`, "com.apple.Safari"},
		{"not a bundle id", true, `[{"LSHandlerURLScheme":"http","LSHandlerRoleAll":"-a /evil"}]`, ""},
		{"unreadable", true, `not json`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := setHome(t)
			b, ft, _ := testBattery(t, "darwin", Config{})
			if c.store {
				path := filepath.Join(home, launchServicesPrefs)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				ft.out["plutil"] = []byte(c.out)
			}
			if got := b.defaultBrowser(); got != c.want {
				t.Fatalf("defaultBrowser() = %q; want %q", got, c.want)
			}
		})
	}
}

// macOS opens the sign-in page in the http handler, not whatever app
// owns .html files.
func TestLaunchBrowserNamesTheBrowserOnMac(t *testing.T) {
	setHome(t)
	b, ft, _ := testBattery(t, "darwin", Config{})
	if err := b.launchBrowser("/s/p.html"); err != nil {
		t.Fatal(err)
	}
	if got := ft.got(); len(got) != 1 || got[0] != "open -b com.apple.Safari /s/p.html" {
		t.Fatalf("argv %q", got)
	}
}
