package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Generated-code gate, the blueprint sibling of
// TestGeneratedCLIPassesRepoVettool: every project emitted from a
// shipped blueprint must pass the repo's own vettool — the whole
// module, `_test.go` files included (go vet analyzes them; `go build`
// never compiles them, and the generator ships e2e/axe tests whose
// defects are the same class as broken app code).
//
// TestExampleBlueprintsGenerateAndCompile proves the emitted Go BUILDS
// under the stdlib vet; this gate adds the repo's analyzer set, so a
// template regression that compiles but trips a repo rule (an
// unbounded response read, a client with no deadline — the shapes the
// round-3/round-5 rules exist for) fails here instead of in every
// customer the generator touches. Baseline recorded 2026-09-07: the
// emitted modules are vettool-clean; the coordinator expects
// clienttimeout/unboundedresp analyzers to start firing on the emitted
// e2e_test.go once their slice merges, and that fire is a finding in
// the template (cmd/gofastr/blueprint.go), not in the blueprint.
//
// Rendered in-process (loadBlueprint + renderBlueprintFiles) into a
// scratch package inside the repo module, so imports resolve with no
// go.mod, no replace, and no network. Deterministic and offline
// (GOPROXY=off); a module-resolution failure skips with a message.

// blueprintVetScratch is the throwaway package each blueprint is
// rendered into. It differs from buildGateScratchPkg and from the
// committed "blueprintgen"/"blueprintbuildgen" scratch names for the
// same reason those two differ from each other: suites that run beside
// each other must not share a scratch directory.
const blueprintVetScratch = "blueprintvetgen"

func TestBlueprintProjectPassesRepoVettool(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks and vets every generated example module; skipped under -short")
	}
	paths := exampleBlueprints(t)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain to vet the generated module: %v", err)
	}
	vettool := filepath.Join(t.TempDir(), "vettool")
	build := exec.Command("go", "build", "-o", vettool, "./cmd/vettool")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the repo vettool failed: %v\n%s", err, out)
	}

	for _, path := range paths {
		path := path
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			appDir := renderBlueprintForVet(t, repoRoot, path, name)
			vet := exec.Command("go", "vet", "-vettool="+vettool, "./...")
			vet.Dir = appDir
			vet.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
			out, err := vet.CombinedOutput()
			if err == nil {
				return
			}
			text := string(out)
			for _, marker := range []string{"cannot find module", "finding module for package", "dial tcp", "module lookup disabled"} {
				if strings.Contains(text, marker) {
					t.Skipf("generated module could not be built in this environment: %s", text)
				}
			}
			t.Errorf("GATE: blueprint-generated project does not pass the repo vettool — every diagnostic is a finding in the emitted templates (cmd/gofastr/blueprint.go), not in the blueprint or the customer's code:\n%s", text)
		})
	}
}

// renderBlueprintForVet loads one shipped blueprint, repoints its
// module at the scratch package (so self-imports resolve to the code
// about to be written, not the committed tree next door), renders
// in-process, and writes the files. Returns the app directory the
// module lives in (output_dir aware). Mirrors generateAndCompile
// Blueprint's rewrite, without the generator subprocess.
func renderBlueprintForVet(t *testing.T, repoRoot, blueprintPath, name string) string {
	t.Helper()

	exampleDir := filepath.Dir(blueprintPath)
	dir := filepath.Join(exampleDir, blueprintVetScratch)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear stale scratch: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	src, err := os.ReadFile(blueprintPath)
	if err != nil {
		t.Fatalf("read %s: %v", blueprintPath, err)
	}
	realModule := "github.com/DonaldMurillo/gofastr/examples/" + name
	moduleLine := "module: " + realModule
	if !strings.Contains(string(src), moduleLine) {
		t.Fatalf("%s no longer declares %q — update this test's rewrite", blueprintPath, moduleLine)
	}
	scratchYAML := filepath.Join(dir, "gofastr.yml")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	rewritten := strings.Replace(string(src), moduleLine, moduleLine+"/"+blueprintVetScratch, 1)
	if err := os.WriteFile(scratchYAML, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("write scratch blueprint: %v", err)
	}

	bp, err := loadBlueprint(scratchYAML)
	if err != nil {
		t.Fatalf("load rewritten blueprint: %v", err)
	}
	appDir := dir
	if out := blueprintOutputDir(string(src)); out != "" {
		appDir = filepath.Join(dir, out)
	}
	for _, f := range mustRenderBlueprintFiles(t, bp) {
		full := filepath.Join(appDir, f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return appDir
}
