package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The vetgate's own fixtures carry no states entity; this leg renders the
// entity-mode module WITH moves and runs the same repo vettool over it.
func TestGeneratedCLIWithStatesPassesRepoVettool(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain: %v", err)
	}
	spec, err := buildCLISpec(statesFixtureDecls(), cliOptions{binary: "myapp"}, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	files := renderCLIFiles(spec)

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	// The entity-mode CLI imports the app's entities/client package; the
	// real one comes from the same generator (renderClient), so the
	// module under vet is exactly the pair a generated app ships.
	if err := os.MkdirAll(filepath.Join(dir, "entities", "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "entities", "client", "client.go"), renderClient(statesFixtureDecls()))
	for _, f := range files {
		full := filepath.Join(dir, "cli", f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	vettool := filepath.Join(dir, "vettool")
	build := exec.Command("go", "build", "-o", vettool, "./cmd/vettool")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the repo vettool failed: %v\n%s", err, out)
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
