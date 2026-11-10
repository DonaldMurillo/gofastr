package desktopcmd

import (
	"debug/macho"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// archFixtureApp is a module whose binary prints the architecture it
// was compiled for, so a test can ask each slice of a bundle who it is.
func archFixtureApp(t *testing.T, dir string) {
	t.Helper()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module deskfixture\n\ngo 1.21\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "main.go"),
		"package main\n\nimport (\n\t\"fmt\"\n\t\"runtime\"\n)\n\nfunc main() { fmt.Println(\"slice \" + runtime.GOARCH) }\n")
}

// buildArchFixture runs the macOS desktop build of archFixtureApp with
// GOARCH set to goarch ("" for the default) and returns the bundle's
// executable.
func buildArchFixture(t *testing.T, goarch string, extra ...string) string {
	t.Helper()
	t.Setenv("GOARCH", goarch)
	fixture := t.TempDir()
	archFixtureApp(t, fixture)
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(fixture); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	out := filepath.Join(fixture, "dist")
	args := append([]string{"--id", "dev.gofastr.arch", "--name", "Arch", "--pkg", ".", "-o", out}, extra...)
	captured, code := verbOutput(t, func() { runDesktopBuildForOS(args, "darwin") })
	if code != -1 {
		t.Fatalf("desktop build exited %d:\n%s", code, captured)
	}
	return filepath.Join(out, "Arch.app", "Contents", "MacOS", "Arch")
}

// The default macOS build is universal (#604): one executable holding
// an arm64 and an amd64 slice, signed as a whole, and each slice runs.
func TestMacBuildDefaultIsUniversal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("merging the slices needs lipo, a macOS tool")
	}
	if out, err := exec.Command("lipo", "-info", "/usr/bin/true").CombinedOutput(); err != nil {
		t.Skipf("no working lipo on this host: %v\n%s", err, out)
	}
	exe := buildArchFixture(t, "")

	fat, err := macho.OpenFat(exe)
	if err != nil {
		t.Fatalf("the bundle executable is not a universal Mach-O: %v", err)
	}
	var cpus []macho.Cpu
	for _, a := range fat.Arches {
		cpus = append(cpus, a.Cpu)
	}
	_ = fat.Close()
	slices.Sort(cpus)
	if want := []macho.Cpu{macho.CpuAmd64, macho.CpuArm64}; !slices.Equal(cpus, want) {
		t.Fatalf("slices = %v, want %v", cpus, want)
	}

	// The signature covers both slices: codesign checks the one named.
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	for _, arch := range []string{"arm64", "x86_64"} {
		if out, err := exec.Command("codesign", "--verify", "--strict", "--arch", arch, bundle).CombinedOutput(); err != nil {
			t.Errorf("the %s slice does not verify: %v\n%s", arch, err, out)
		}
	}

	// Each slice runs and is the architecture it claims. The Intel one
	// needs an Intel Mac or Rosetta; the arm64 one needs Apple silicon.
	for flag, want := range map[string]string{"-arm64": "slice arm64", "-x86_64": "slice amd64"} {
		if exec.Command("arch", flag, "/usr/bin/true").Run() != nil {
			t.Logf("this host cannot run %s binaries; that slice is checked by its header only", flag)
			continue
		}
		out, err := exec.Command("arch", flag, exe).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("arch %s ran %q (%v), want %q", flag, strings.TrimSpace(string(out)), err, want)
		}
	}
}

// GOARCH picks one architecture: the bundle then holds a thin Mach-O
// for it, and the build never asks for lipo.
func TestMacBuildGOARCHPicksOneSlice(t *testing.T) {
	for goarch, want := range map[string]macho.Cpu{"arm64": macho.CpuArm64, "amd64": macho.CpuAmd64} {
		t.Run(goarch, func(t *testing.T) {
			calls := fakeRunTool(t, nil)
			exe := buildArchFixture(t, goarch, "--no-sign")
			f, err := macho.Open(exe)
			if err != nil {
				t.Fatalf("the bundle executable is not a thin Mach-O: %v", err)
			}
			defer f.Close()
			if f.Cpu != want {
				t.Fatalf("cpu = %v, want %v", f.Cpu, want)
			}
			if len(*calls) != 0 {
				t.Fatalf("a single-architecture build ran tools: %v", *calls)
			}
		})
	}
}
