package initcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"golang.org/x/mod/modfile"
)

func TestInitGoDirectives(t *testing.T) {
	// Read the release's requirements instead of the local go command's version.
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "go.mod"))
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
			oldVersion := cli.Version
			cli.Version = release
			t.Cleanup(func() { cli.Version = oldVersion })
			clitest.CovT_chdir(t, t.TempDir())
			clitest.CovT_capStdout(t, func() { RunInit([]string{"app", "--no-entity"}) })
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
			// The repo's go.mod has no toolchain line while it would only
			// repeat the go line; the scaffold must match either way.
			if (got.Toolchain == nil) != (want.Toolchain == nil) ||
				(want.Toolchain != nil && got.Toolchain.Name != want.Toolchain.Name) {
				t.Errorf("scaffold toolchain line must match the release go.mod's:\n%s", data)
			}
			if release != "dev" && (len(got.Require) != 1 || got.Require[0].Mod.Path != project.GofastrModule || got.Require[0].Mod.Version != "v"+release) {
				t.Errorf("scaffold lost release pin:\n%s", data)
			}
		})
	}
}

func TestProjectNameWindowsDevices(t *testing.T) {
	for _, name := range []string{"con", "prn", "aux", "nul", "com0", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "lpt0", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9"} {
		if err := validateProjectName(name); err == nil || !strings.Contains(err.Error(), "reserved") {
			t.Errorf("validateProjectName(%q) must explain Windows device name refusal, got %v", name, err)
		}
	}
	for _, name := range []string{"console", "null", "auxiliary", "com10", "lpt10", "con-app", "app_nul", "."} {
		if err := validateProjectName(name); err != nil {
			t.Errorf("validateProjectName(%q): %v", name, err)
		}
	}
}

func TestInitKeepsHigherExistingGoRequirements(t *testing.T) {
	oldVersion := cli.Version
	cli.Version = "dev"
	t.Cleanup(func() { cli.Version = oldVersion })
	t.Setenv("GOTOOLCHAIN", "local")
	for _, toolchain := range []string{"go1.27.4", "default"} {
		t.Run(toolchain, func(t *testing.T) {
			dir := t.TempDir()
			clitest.CovT_chdir(t, dir)
			initial := "module local/existing\n\ngo 1.27.3\n\ntoolchain " + toolchain + "\n\nrequire example.com/existing v1.0.0\n"
			if err := os.WriteFile("go.mod", []byte(initial), 0o644); err != nil {
				t.Fatal(err)
			}
			clitest.CovT_capStdout(t, func() { RunInit([]string{".", "--module=local/existing", "--no-entity"}) })
			data, err := os.ReadFile("go.mod")
			if err != nil {
				t.Fatal(err)
			}
			mod, err := modfile.Parse("go.mod", data, nil)
			if err != nil {
				t.Fatal(err)
			}
			if mod.Go.Version != "1.27.3" || mod.Toolchain == nil || mod.Toolchain.Name != toolchain {
				t.Fatalf("init . changed existing Go requirements:\n%s", data)
			}
			if len(mod.Require) != 1 || mod.Require[0].Mod.Path != "example.com/existing" {
				t.Fatalf("init . lost an existing dependency:\n%s", data)
			}
		})
	}
}

// An existing go line above the framework's floor survives the edit step,
// with no toolchain line written to repeat it.
func TestInitKeepsGoAboveFloor(t *testing.T) {
	oldVersion := cli.Version
	cli.Version = "dev"
	t.Cleanup(func() { cli.Version = oldVersion })
	t.Setenv("GOTOOLCHAIN", "local")
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	if err := os.WriteFile("go.mod", []byte("module local/existing\n\ngo 1.99.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_capStdout(t, func() { RunInit([]string{".", "--module=local/existing", "--no-entity"}) })
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mod.Go.Version != "1.99.0" || mod.Toolchain != nil {
		t.Fatalf("go.mod = go %s, toolchain %v; want go 1.99.0 and no toolchain line:\n%s",
			mod.Go.Version, mod.Toolchain, data)
	}
}

// An existing module below the framework's floor is raised to it, and the
// toolchain line that would only repeat the go line is dropped, as
// go mod tidy would drop it.
func TestInitRaisesGoBelowFloor(t *testing.T) {
	oldVersion := cli.Version
	cli.Version = "dev"
	t.Cleanup(func() { cli.Version = oldVersion })
	t.Setenv("GOTOOLCHAIN", "local")
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	initial := "module local/existing\n\ngo 1.27.2\n\ntoolchain go1.27.2\n"
	if err := os.WriteFile("go.mod", []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_capStdout(t, func() { RunInit([]string{".", "--module=local/existing", "--no-entity"}) })
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mod.Go.Version != frameworkGoDirective || mod.Toolchain != nil {
		t.Fatalf("go.mod = go %s, toolchain %v; want go %s and no toolchain line:\n%s",
			mod.Go.Version, mod.Toolchain, frameworkGoDirective, data)
	}
}

func TestScaffoldGoRequirementsToolchainMeetsHigherMinimum(t *testing.T) {
	dir := t.TempDir()
	// Parsing an existing future minimum must not download that toolchain.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module local/future\n\ngo 1.99.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	minimum, preferred, err := scaffoldGoRequirements(dir)
	if err != nil {
		t.Fatal(err)
	}
	if minimum != "1.99.0" || preferred != "go1.99.0" {
		t.Fatalf("requirements = %q, %q; want future minimum and matching toolchain", minimum, preferred)
	}
}
