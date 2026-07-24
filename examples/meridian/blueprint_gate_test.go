package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scratchPkg is the throwaway package the blueprint is generated into.
// It lives inside the repo module so the generated code's self-import
// (github.com/DonaldMurillo/gofastr/examples/meridian/<scratchPkg>/entities)
// resolves without a go.mod, a replace directive, or a network fetch.
// Gitignored; removed before and after the test so a killed run cannot
// leave a package behind that later trips `go build ./...`.
const scratchPkg = "blueprintgen"

// TestBlueprintStillGenerates compiles gofastr.yml.
//
// Meridian's checked-in Go is hand-maintained and deliberately no longer
// matches generator output (see doc.go), so — unlike examples/ecommerce —
// there is nothing here to regenerate in place. What still has to hold is
// weaker but not nothing: the blueprint that seeded this app must keep
// producing an app that BUILDS. Without this gate, gofastr.yml can rot
// silently, which is exactly how #131 went unnoticed.
//
// The test does not compare the output to anything. Byte-parity with the
// checked-in files is a non-goal; buildability is the contract.
func TestBlueprintStillGenerates(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the generator and compiles its output; skipped under -short")
	}

	dir, err := filepath.Abs(scratchPkg)
	if err != nil {
		t.Fatalf("abs %s: %v", scratchPkg, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("clear stale %s: %v", scratchPkg, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", scratchPkg, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	// Copy the blueprint in, repointing app.module at the scratch package
	// so the generated self-imports resolve to the code just emitted
	// rather than to the hand-maintained package next door.
	src, err := os.ReadFile("gofastr.yml")
	if err != nil {
		t.Fatalf("read gofastr.yml: %v", err)
	}
	const realModule = "github.com/DonaldMurillo/gofastr/examples/meridian"
	moduleLine := "module: " + realModule
	if !strings.Contains(string(src), moduleLine) {
		t.Fatalf("gofastr.yml no longer declares %q — update this test's rewrite", moduleLine)
	}
	rewritten := strings.Replace(string(src), moduleLine, moduleLine+"/"+scratchPkg, 1)
	if err := os.WriteFile(filepath.Join(dir, "gofastr.yml"), []byte(rewritten), 0o644); err != nil {
		t.Fatalf("write scratch blueprint: %v", err)
	}

	// Generate with the in-tree CLI source, so a generator regression
	// fails here rather than at the next release.
	gen := exec.Command("go", "run", "github.com/DonaldMurillo/gofastr/cmd/gofastr",
		"generate", "--from=gofastr.yml")
	gen.Dir = dir
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("gofastr generate --from=gofastr.yml: %v\n%s", err, out)
	}

	// Compile the result. The binary goes to a temp dir — never the
	// worktree. Building the main package pulls in the generated
	// entities package transitively; ./entities/... covers the rest
	// (non-main packages compile without emitting artifacts).
	bin := filepath.Join(t.TempDir(), "meridian-from-blueprint")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("generated app does not compile — gofastr.yml has rotted:\n%s", out)
	}

	rest := exec.Command("go", "build", "./entities/...")
	rest.Dir = dir
	if out, err := rest.CombinedOutput(); err != nil {
		t.Fatalf("generated entities packages do not compile:\n%s", out)
	}
}
