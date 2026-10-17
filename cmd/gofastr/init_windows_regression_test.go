package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

func TestInitFreshDirectory(t *testing.T) {
	bin := buildGofastrBin(t)
	work := t.TempDir()
	// Exercise the real CLI with automatic toolchain switching disabled.
	// Setting the directives must succeed before a later tidy switches Go.
	t.Setenv("GOTOOLCHAIN", "local")
	for i := range 3 {
		name := fmt.Sprintf("repro-check-%d", i)
		cmd := exec.Command(bin, "init", name, "--module=local/"+name)
		cmd.Dir = work
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fresh init %d: %v\n%s", i, err, out)
		}
		if strings.Count(string(out), "Created project "+name) != 1 || strings.Contains(string(out), "already exists") {
			t.Fatalf("fresh init %d did not succeed exactly once:\n%s", i, out)
		}
		for _, file := range []string{"main.go", "screens.go", "entities/entities.go", "migrations/.gitkeep", ".env", ".gitignore", ".git", "go.mod"} {
			if _, err := os.Stat(filepath.Join(work, name, file)); err != nil {
				t.Errorf("missing scaffold file %s: %v", file, err)
			}
		}
	}
	// A second invocation must refuse the existing project without touching it.
	sentinel := filepath.Join(work, "repro-check-0", "main.go")
	if err := os.WriteFile(sentinel, []byte("keep this file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "init", "repro-check-0")
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "already exists") {
		t.Fatalf("repeat init should refuse with exit 1: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep this file\n" {
		t.Fatalf("repeat init changed existing file: %q, %v", got, err)
	}
}

func TestInitGoDirectives(t *testing.T) {
	// Read the release's requirements instead of the local go command's version.
	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOTOOLCHAIN", "local")
	for _, release := range []string{"dev", "0.88.1"} {
		t.Run(release, func(t *testing.T) {
			oldVersion := version
			version = release
			t.Cleanup(func() { version = oldVersion })
			covT_chdir(t, t.TempDir())
			covT_capStdout(t, func() { runInit([]string{"app", "--no-entity"}) })
			data, err := os.ReadFile(filepath.Join("app", "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Go == nil || got.Go.Version != want.Go.Version {
				t.Errorf("scaffold must use release go %s:\n%s", want.Go.Version, data)
			}
			if got.Toolchain == nil || got.Toolchain.Name != want.Toolchain.Name {
				t.Errorf("scaffold must use release toolchain %s:\n%s", want.Toolchain.Name, data)
			}
			if release != "dev" && (len(got.Require) != 1 || got.Require[0].Mod.Path != gofastrModule || got.Require[0].Mod.Version != "v"+release) {
				t.Errorf("scaffold lost release pin:\n%s", data)
			}
		})
	}
}

func TestProjectNameWindowsDevices(t *testing.T) {
	for _, name := range []string{"con", "prn", "aux", "nul", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9"} {
		if err := validateProjectName(name); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("validateProjectName(%q) must explain Windows device name refusal, got %v", name, err)
		}
	}
	for _, name := range []string{"console", "null", "auxiliary", "com0", "com10", "lpt0", "lpt10", "con-app", "app_nul", "."} {
		if err := validateProjectName(name); err != nil {
			t.Errorf("validateProjectName(%q): %v", name, err)
		}
	}
}
