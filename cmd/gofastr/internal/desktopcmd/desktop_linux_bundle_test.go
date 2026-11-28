package desktopcmd

import (
	"debug/elf"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestLinuxDesktopBuildRejectsUnsupportedOptions(t *testing.T) {
	t.Setenv("GOARCH", "amd64")
	for _, test := range []struct {
		name  string
		flags desktopBuildFlags
		exe   string
		want  string
	}{
		{"sign", desktopBuildFlags{sign: "identity"}, "App", "macOS-only"},
		{"no-sign", desktopBuildFlags{noSign: true}, "App", "macOS-only"},
		{"notarize", desktopBuildFlags{notarize: true}, "App", "macOS-only"},
		{"entitlements", desktopBuildFlags{entitlements: "rights.plist"}, "App", "macOS-only"},
		{"invalid scheme", desktopBuildFlags{scheme: "Invalid Scheme"}, "App", "--scheme"},
		{"icon", desktopBuildFlags{icon: "icon.png"}, "App", "icon packaging"},
		{"dot", desktopBuildFlags{}, ".", "--name must name an executable"},
		{"parent", desktopBuildFlags{}, "..", "--name must name an executable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := buildLinuxDesktop(test.flags, test.exe); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("unsupported build: err = %v, want one naming %q", err, test.want)
			}
		})
	}
	t.Setenv("GOARCH", "386")
	if err := buildLinuxDesktop(desktopBuildFlags{}, "App"); err == nil || !strings.Contains(err.Error(), "amd64 and arm64") {
		t.Fatalf("unsupported architecture: err = %v", err)
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
			clitest.CovT_capOutput(t, func() {
				code = clitest.CovT_capExit(t, func() {
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
