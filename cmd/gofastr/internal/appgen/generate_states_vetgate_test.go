package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// The vetgate's own fixtures carry no states entity; this leg renders the
// entity-mode module WITH moves and runs the same repo vettool over it.
//
// It builds ./cmd/vettool in a subprocess, which no import edge shows,
// so an analyzer change has to name it to select this package.
//
//gofastr:test-reads internal/analyzers/ cmd/vettool/
func TestGeneratedCLIWithStatesPassesRepoVettool(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain: %v", err)
	}
	spec, err := buildCLISpec(statesFixtureDecls(), CliOptions{Binary: "myapp"}, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	files := RenderCLIFiles(spec)

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	// The entity-mode CLI imports the app's entities/client package; the
	// real one comes from the same generator (renderClient), so the
	// module under vet is exactly the pair a generated app ships.
	if err := os.MkdirAll(filepath.Join(dir, "entities", "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "entities", "client", "client.go"), renderClient(statesFixtureDecls()))
	for _, f := range files {
		full := filepath.Join(dir, "cli", f.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vettool, err := clitest.RepoVettool()
	if err != nil {
		t.Fatal(err)
	}
	vet := exec.Command("go", "vet", "-vettool="+vettool, "./...")
	vet.Dir = dir
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
	t.Errorf("GATE: states-fixture generated CLI does not pass the repo vettool:\n%s", text)
}
