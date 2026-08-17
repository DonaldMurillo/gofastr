package main

import (
	"bytes"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExampleBlueprintsLoad validates every examples/<name>/gofastr.yml parses
// and validates cleanly. This is the cheap half of the gate: it runs under
// -short and catches a blueprint that no longer decodes.
//
// It is NOT sufficient on its own. Parsing proves nothing about the Go the
// generator emits from the parse — see TestExampleBlueprintsGenerateAndCompile.
func TestExampleBlueprintsLoad(t *testing.T) {
	for _, path := range exampleBlueprints(t) {
		path := path
		t.Run(filepath.Base(filepath.Dir(path)), func(t *testing.T) {
			if _, err := loadBlueprint(path); err != nil {
				t.Errorf("%s failed to load: %v", path, err)
			}
		})
	}
}

// buildGateScratchPkg is the throwaway package each blueprint is generated
// into. It lives inside the repo module so the generated code's self-imports
// (github.com/DonaldMurillo/gofastr/examples/<name>/<scratch>/entities) resolve
// without a go.mod, a replace directive, or a network fetch.
//
// The name deliberately differs from the "blueprintgen" used by
// examples/{ecommerce,meridian}/blueprint_gate_test.go: those packages run as
// their own test binaries, so a shared directory name would collide when the
// suites run beside each other under `go test -p 2`.
const buildGateScratchPkg = "blueprintbuildgen"

// exampleBlueprints returns every examples/<name>/gofastr.yml in the repo.
func exampleBlueprints(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("../../examples/*/gofastr.yml")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Skip("no example blueprints found (run from repo)")
	}
	return matches
}

// TestExampleBlueprintsGenerateAndCompile is the gate that matters: every
// shipped blueprint must emit Go that BUILDS.
//
// Why this exists, generalized, rather than per-example:
//
// examples/meridian/blueprint_gate_test.go already made exactly this argument
// for one blueprint ("Without this gate, gofastr.yml can rot silently, which is
// exactly how #131 went unnoticed"), and examples/ecommerce reaches the same
// guarantee a different way — its committed app/ is compiled by `go build
// ./...` and a byte-parity test pins the generator to it.
//
// Those two were the ONLY blueprints whose emitted Go was ever compiled. The
// other five — blog, lms, portfolio, project-manager, real-estate — were
// covered solely by TestExampleBlueprintsLoad above, which checks that the YAML
// parses. All five emitted code that did not compile, and neither of the two
// gated examples could catch it: ecommerce declares no home screen and no
// `access:` role policy, so it is the one example that never reaches either
// broken generator path.
//
// The lesson is that a gate aimed at one fixture proves one fixture. This test
// is aimed at all seven, so a generator path is covered the moment any
// blueprint uses it.
func TestExampleBlueprintsGenerateAndCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the generator and compiles its output; skipped under -short")
	}
	for _, path := range exampleBlueprints(t) {
		path := path
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			generateAndCompileBlueprint(t, path, name)
		})
	}
}

// TestExampleBlueprintsBoot is the top rung of the blueprint ladder:
// load → generate → compile → START the binary and serve. Compile-only
// was not enough: the blog blueprint rotted for a whole release while
// passing all three lower rungs — its seed seeded `post_id: 1` into a
// UUID relation column, an app that builds cleanly and dies at boot
// ("seed hooks: seed comments: validation failed"). Nothing below the
// boot rung can see a runtime failure, so this test is the gate that
// would have caught it.
//
// Failure modes asserted: non-zero exit before serving (the seed-death
// class), failure to bind/serve within the deadline, and error output on
// the way down. The child runs in its own process group and is killed by
// tree even when the test fails mid-flight, so no app leaks past the run
// (CI-safe on Linux and macOS via configureTestProcessGroup).
func TestExampleBlueprintsBoot(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, compiles, and boots every example blueprint; skipped under -short")
	}
	for _, path := range exampleBlueprints(t) {
		path := path
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			bin, appDir := generateAndCompileBlueprint(t, path, name)
			bootGeneratedApp(t, name, bin, appDir)
		})
	}
}

// bootGeneratedApp starts a generated app binary on a free port and waits
// for it to serve. A process that exits first (seed validation, failed
// migration, refused bind) fails immediately with its captured output —
// faster than burning the whole HTTP deadline, and the output is the
// diagnostic that matters.
func bootGeneratedApp(t *testing.T, name, bin, appDir string) {
	t.Helper()
	addr := freeAddr(t)
	cmd := exec.Command(testExecutablePath(bin))
	cmd.Dir = appDir
	cmd.Env = append(os.Environ(),
		"PORT="+addr,
		// Scratch-scoped DB file: the scratch dir's cleanup removes it, so
		// repeated runs never inherit a stale, already-seeded database.
		"DATABASE_URL=file:"+filepath.Join(appDir, "boot-gate.db"),
	)
	configureTestProcessGroup(cmd)
	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start generated %s app: %v", name, err)
	}
	t.Cleanup(func() {
		_ = killTestProcessTree(cmd)
		_, _ = cmd.Process.Wait()
	})
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	baseURL := "http://" + addr
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-exited:
			t.Fatalf("generated %s app exited before serving (err=%v):\n%s", name, err, output.String())
		default:
		}
		resp, err := http.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			// Any non-server-error answer proves the app is up and routing —
			// auth-gated apps answer 302/401 on /, a screen-less one 404.
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("generated %s app did not serve at %s within 90s:\n%s", name, baseURL, output.String())
}

// generateAndCompileBlueprint generates one blueprint into a scratch package
// beside it, compiles every package it emitted, and returns the built app
// binary plus the directory it should run from (output_dir aware).
func generateAndCompileBlueprint(t *testing.T, blueprintPath, name string) (string, string) {
	t.Helper()

	exampleDir := filepath.Dir(blueprintPath)
	dir, err := filepath.Abs(filepath.Join(exampleDir, buildGateScratchPkg))
	if err != nil {
		t.Fatalf("abs scratch dir: %v", err)
	}
	// Removed before AND after, so a killed run cannot leave a package behind
	// that later trips `go build ./...` at the repo root.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear stale scratch: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir scratch: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	src, err := os.ReadFile(blueprintPath)
	if err != nil {
		t.Fatalf("read %s: %v", blueprintPath, err)
	}
	// Repoint app.module at the scratch package so the generated self-imports
	// resolve to the code just emitted rather than to whatever package is
	// committed next door.
	realModule := "github.com/DonaldMurillo/gofastr/examples/" + name
	moduleLine := "module: " + realModule
	if !strings.Contains(string(src), moduleLine) {
		t.Fatalf("%s no longer declares %q — update this test's rewrite", blueprintPath, moduleLine)
	}
	rewritten := strings.Replace(string(src), moduleLine, moduleLine+"/"+buildGateScratchPkg, 1)
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(rewritten), 0o644); err != nil {
		t.Fatalf("write scratch blueprint: %v", err)
	}

	// Generate with the in-tree CLI source, so a generator regression fails
	// here rather than at the next release.
	gen := exec.Command("go", "run", "github.com/DonaldMurillo/gofastr/cmd/gofastr",
		"generate", "--from=gofastr.yml")
	gen.Dir = dir
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate --from=gofastr.yml: %v\n%s", err, out)
	}

	// output_dir moves the emitted app one level down (examples/ecommerce uses
	// it). Resolve it from the blueprint rather than assuming a flat layout.
	appDir := dir
	if out := blueprintOutputDir(string(src)); out != "" {
		appDir = filepath.Join(dir, out)
	}

	// Compile the main package. The binary goes to a temp dir — never the
	// worktree. This pulls in the generated entities package transitively;
	// ./... below covers any package the main package does not import.
	bin := filepath.Join(t.TempDir(), name+"-from-blueprint")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = appDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated app does not compile — the generator emitted broken Go for %s:\n%s", blueprintPath, out)
	}

	rest := exec.Command("go", "build", "./...")
	rest.Dir = appDir
	if out, err := rest.CombinedOutput(); err != nil {
		t.Fatalf("generated packages do not compile for %s:\n%s", blueprintPath, out)
	}

	// `go vet` reaches the emitted _test.go files, which `go build` never
	// compiles. The generator ships e2e/axe tests; broken ones are the same
	// class of defect as broken app code and should fail the same way.
	vet := exec.Command("go", "vet", "./...")
	vet.Dir = appDir
	if out, err := vet.CombinedOutput(); err != nil {
		t.Fatalf("generated code fails go vet for %s:\n%s", blueprintPath, out)
	}
	return bin, appDir
}

// blueprintOutputDir extracts an uncommented `output_dir:` from a blueprint.
// Returns "" when the blueprint scaffolds into the module root.
func blueprintOutputDir(blueprint string) string {
	for _, line := range strings.Split(blueprint, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		_, after, found := strings.Cut(trimmed, "output_dir:")
		if !found {
			continue
		}
		if v := strings.TrimSpace(after); v != "" {
			return v
		}
	}
	return ""
}
