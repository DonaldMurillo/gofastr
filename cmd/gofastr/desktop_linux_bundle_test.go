package main

import (
	"debug/elf"
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxDesktopBuildRejectsUnsupportedOptions(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	for _, test := range []struct {
		name  string
		flags desktopBuildFlags
		exe   string
	}{
		{"sign", desktopBuildFlags{sign: "identity"}, "App"},
		{"no-sign", desktopBuildFlags{noSign: true}, "App"},
		{"notarize", desktopBuildFlags{notarize: true}, "App"},
		{"entitlements", desktopBuildFlags{entitlements: "rights.plist"}, "App"},
		{"scheme", desktopBuildFlags{scheme: "example"}, "App"},
		{"icon", desktopBuildFlags{icon: "icon.png"}, "App"},
		{"dot", desktopBuildFlags{}, "."},
		{"parent", desktopBuildFlags{}, ".."},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := buildLinuxDesktop(test.flags, test.exe); err == nil {
				t.Fatal("unsupported build accepted")
			}
		})
	}
	t.Setenv("GOARCH", "386")
	if err := buildLinuxDesktop(desktopBuildFlags{}, "App"); err == nil {
		t.Fatal("unsupported architecture accepted")
	}
}
func TestDesktopBuildProducesLinuxELF(t *testing.T) {
	fixture := t.TempDir()
	desktopFixtureApp(t, fixture)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(fixture); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	for _, test := range []struct {
		arch    string
		machine elf.Machine
	}{
		{"amd64", elf.EM_X86_64},
		{"arm64", elf.EM_AARCH64},
	} {
		t.Run(test.arch, func(t *testing.T) {
			t.Setenv("GOARCH", test.arch)
			out := filepath.Join(fixture, "dist", test.arch)
			var code int
			covT_capStdout(t, func() {
				code = covT_capExit(t, func() {
					runDesktopBuildForOS([]string{"--id=dev.gofastr.linux", "--name=Linux Fixture", "-o", out}, "linux")
				})
			})
			if code != -1 {
				t.Fatalf("exit %d", code)
			}
			file, err := elf.Open(filepath.Join(out, "Linux Fixture"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if file.Machine != test.machine {
				t.Fatalf("machine %s, want %s", file.Machine, test.machine)
			}
			if _, err = os.Stat(filepath.Join(out, "Linux Fixture.app")); !os.IsNotExist(err) {
				t.Fatalf("unexpected macOS bundle: %v", err)
			}
		})
	}
}
