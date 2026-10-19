package main

import (
	"github.com/DonaldMurillo/gofastr/internal/scratchdir"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Generated-code gate, the app spec sibling of
// TestGeneratedCLIPassesRepoVettool: every project emitted from a
// shipped app spec must pass the repo's own vettool — the whole
// module, `_test.go` files included (go vet analyzes them; `go build`
// never compiles them, and the generator ships e2e/axe tests whose
// defects are the same class as broken app code).
//
// TestExampleAppSpecsGenerateAndCompile proves the emitted Go BUILDS
// under the stdlib vet; this gate adds the repo's analyzer set, so a
// template regression that compiles but trips a repo rule (an
// unbounded response read, a client with no deadline — the shapes the
// round-3/round-5 rules exist for) fails here instead of in every
// customer the generator touches. Baseline recorded 2026-09-07: the
// emitted modules are vettool-clean; the coordinator expects
// clienttimeout/unboundedresp analyzers to start firing on the emitted
// e2e_test.go once their slice merges, and that fire is a finding in
// the template (cmd/gofastr/appspec.go), not in the app spec.
//
// Rendered in-process (loadAppSpec + renderAppSpecFiles) into a
// scratch package inside the repo module, so imports resolve with no
// go.mod, no replace, and no network. Deterministic and offline
// (GOPROXY=off); a module-resolution failure skips with a message.

// appSpecVetScratch prefixes the throwaway package each app spec is
// rendered into, under scratchdir.Root. It differs from the build gate's
// prefix because the two suites run beside each other and must not share a
// scratch directory.
const appSpecVetScratch = "vet_"

func TestAppSpecProjectPassesRepoVettool(t *testing.T) {
	if testing.Short() {
		t.Skip("type-checks and vets every generated example module; skipped under -short")
	}
	paths := exampleAppSpecs(t)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain to vet the generated module: %v", err)
	}
	vettool := testExecutablePath(filepath.Join(t.TempDir(), "vettool"))
	build := exec.Command("go", "build", "-o", vettool, "./cmd/vettool")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the repo vettool failed: %v\n%s", err, out)
	}

	for _, path := range paths {
		path := path
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			// Per-example scratch dir; the vettool binary is shared read-only.
			t.Parallel()
			appDir := renderAppSpecForVet(t, repoRoot, path, name)
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
			t.Errorf("GATE: generated project does not pass the repo vettool — every diagnostic is a finding in the emitted templates (cmd/gofastr/appspec.go), not in the app spec or the customer's code:\n%s", text)
		})
	}
}

// renderAppSpecForVet loads one shipped app spec, repoints its
// module at the scratch package (so self-imports resolve to the code
// about to be written, not the committed tree next door), renders
// in-process, and writes the files. Returns the app directory the
// module lives in (output_dir aware). Mirrors generateAndCompile
// App spec's rewrite, without the generator subprocess.
func renderAppSpecForVet(t *testing.T, repoRoot, appSpecPath, name string) string {
	t.Helper()

	dir, scratchModule := scratchdir.Package(t, appSpecVetScratch+name)

	src, err := os.ReadFile(appSpecPath)
	if err != nil {
		t.Fatalf("read %s: %v", appSpecPath, err)
	}
	realModule := "github.com/DonaldMurillo/gofastr/examples/" + name
	moduleLine := "module: " + realModule
	if !strings.Contains(string(src), moduleLine) {
		t.Fatalf("%s no longer declares %q — update this test's rewrite", appSpecPath, moduleLine)
	}
	scratchYAML := filepath.Join(dir, "gofastr.yml")
	rewritten := strings.Replace(string(src), moduleLine, "module: "+scratchModule, 1)
	if err := os.WriteFile(scratchYAML, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("write scratch app spec: %v", err)
	}

	bp, err := loadAppSpec(scratchYAML)
	if err != nil {
		t.Fatalf("load rewritten app spec: %v", err)
	}
	appDir := dir
	if out := appSpecOutputDir(string(src)); out != "" {
		appDir = filepath.Join(dir, out)
	}
	for _, f := range mustRenderAppSpecFiles(t, bp) {
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
