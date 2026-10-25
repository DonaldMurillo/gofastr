package packcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// packTreeModule is the shared app's Go module path.
const packTreeModule = "example.com/packtree"

// packTreeYAML is the one app this package's generated-app tests build:
// the control-block screen TestAppSpecYAMLControlBlocksBuild checks, the
// display and states entities TestDisplayStatesRoundTripThroughGo registers,
// and the seeds the two seed tests boot (an invoice seeded past its initial
// state, a product whose currency is a reference to a declared key). The
// pieces are the same text those tests check in-process, so the build
// checks what they render.
const packTreeYAML = `app:
  name: PackTree
  module: ` + packTreeModule + `
  db:
    driver: sqlite
    url: file:gofastr.db
entities:
  - name: posts
    fields:
      - name: title
        type: string
        required: true
` + displayStatesEntities + seedRefKeyEntities + `screens:
  - name: about
    route: /about
    title: About
` + controlBlocksBody + `  - name: invoices
    route: /invoices
    body:
      - kind: entity_list
        entity: invoices
        fields: [number, status]
        create: true
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: products
        fields: [name]
seed:
  - entity: invoices
    rows:
      - number: INV-0001
        status: draft
      - number: INV-0002
        status: paid
        paid_on: "2026-01-15"
  - entity: currencies
    rows:
      - currency_code: EUR
        label: Euro
  - entity: products
    rows:
      - name: Lamp
        currency: "@currencies.label=Euro"
`

// packTreeBin is the built app's name in the tree; the leading "_" keeps
// it out of every `./...` walk.
var packTreeBin = clitest.TestExecutablePath("_packtree-app")

// packTree generates packTreeYAML into a temp module, writes the display
// round-trip probe beside it, and builds the app and every package, once
// for the package's test binary. It returns the module directory and the
// app binary. Boots keep their database outside the tree.
func packTree(t *testing.T) (dir, bin string) {
	t.Helper()
	repoRoot := clitest.RepoRootDir(t)
	dir = clitest.SharedTree(t, "packtree", func(dir string) error {
		return generatePackTree(repoRoot, dir)
	})
	return dir, filepath.Join(dir, packTreeBin)
}

func generatePackTree(repoRoot, dir string) error {
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		return err
	}
	goMod := "module " + packTreeModule + "\n\ngo " + goVersion +
		"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o600); err != nil {
		return err
	}
	if err := clitest.CopyGoSum(repoRoot, dir); err != nil {
		return err
	}
	bp, err := decodeAppSpecString(packTreeYAML)
	if err != nil {
		return fmt.Errorf("parse packTreeYAML: %w", err)
	}
	files, err := appgen.RenderAppSpecFiles(bp)
	if err != nil {
		return fmt.Errorf("renderAppSpecFiles: %w", err)
	}
	for _, file := range files {
		full := filepath.Join(dir, file.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return err
		}
		perm := file.Mode
		if perm == 0 {
			perm = 0o600
		}
		if err := os.WriteFile(full, []byte(file.Content), perm); err != nil {
			return err
		}
	}
	probe, err := displayStatesProbe(bp)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "displaystates_test.go"), []byte(probe), 0o600); err != nil {
		return err
	}
	// Tidy once, so the -mod=mod steps below and the tests' `go test`
	// leave go.mod and go.sum as they are.
	for _, args := range [][]string{
		{"mod", "tidy"},
		{"build", "-mod=mod", "-o", filepath.Join(dir, packTreeBin), "."},
		{"build", "-mod=mod", "./..."},
	} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go %v: %w\n%s", args, err, out)
		}
	}
	return nil
}

// packTreeEntity returns the named entity of the decoded app spec, or nil.
func packTreeEntity(bp []framework.EntityDeclaration, name string) *framework.EntityDeclaration {
	for i := range bp {
		if bp[i].Name == name {
			return &bp[i]
		}
	}
	return nil
}

// packTreeScreen returns the named screen of the decoded app spec, or nil.
func packTreeScreen(bp appspec.AppSpec, name string) *appspec.AppSpecScreen {
	for i := range bp.Screens {
		if bp.Screens[i].Name == name {
			return &bp.Screens[i]
		}
	}
	return nil
}

// displayStatesProbe is a test in the generated app's package main: it
// registers the generated entities on a real App and compares the
// registered invoices Display and States to what the YAML decoded to, and
// checks tags kept its advisory states.
func displayStatesProbe(bp appspec.AppSpec) (string, error) {
	invoices := packTreeEntity(bp.Entities, "invoices")
	if invoices == nil || invoices.Display == nil || invoices.States == nil {
		return "", fmt.Errorf("packTreeYAML must declare display and states on invoices")
	}
	wantDisplay, err := json.Marshal(invoices.Display)
	if err != nil {
		return "", err
	}
	wantStates, err := json.Marshal(invoices.States)
	if err != nil {
		return "", err
	}
	return `package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"

	"` + packTreeModule + `/entities"
)

func TestRegisteredDisplayStatesMatchDeclaration(t *testing.T) {
	app := framework.NewApp()
	entities.RegisterAll(app)

	e, err := app.Registry.Get("invoices")
	if err != nil {
		t.Fatalf("get invoices: %v", err)
	}
	var wantDisplay entity.DisplayConfig
	if err := json.Unmarshal([]byte(` + "`" + string(wantDisplay) + "`" + `), &wantDisplay); err != nil {
		t.Fatalf("decode expected display: %v", err)
	}
	if !reflect.DeepEqual(e.Config.Display, &wantDisplay) {
		t.Fatalf("Display mismatch.\n got: %#v\nwant: %#v", e.Config.Display, &wantDisplay)
	}
	var wantStates entity.StatesConfig
	if err := json.Unmarshal([]byte(` + "`" + string(wantStates) + "`" + `), &wantStates); err != nil {
		t.Fatalf("decode expected states: %v", err)
	}
	if !reflect.DeepEqual(e.Config.States, &wantStates) {
		t.Fatalf("States mismatch.\n got: %#v\nwant: %#v", e.Config.States, &wantStates)
	}

	tag, err := app.Registry.Get("tags")
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}
	if tag.Config.States == nil || !tag.Config.States.Advisory {
		t.Fatalf("tags states advisory lost: %#v", tag.Config.States)
	}
}
`, nil
}
