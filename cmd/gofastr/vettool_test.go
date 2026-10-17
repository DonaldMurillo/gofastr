package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Under `go test`, runGoVet's os.Executable is this test binary, and
// TestMain (theme_edit_e2e_test.go) hands it to the analyzer driver when
// vet starts it as a vettool, so these tests drive the real path.

// writeVetApp writes a stdlib-only app module (no cmd/vettool) whose
// main calls the http.Get sugar, a clienttimeout finding, with marker
// on the line above the call.
func writeVetApp(t *testing.T, marker string) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo "+goVersion+"\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import "net/http"

func main() {
	`+marker+`
	resp, err := http.Get("http://127.0.0.1:1/")
	if err == nil {
		resp.Body.Close()
	}
}
`)
	return dir
}

func TestVerifyVetsAppsWithSuite(t *testing.T) {
	out, ok := runGoVet(writeVetApp(t, ""), true)
	if ok {
		t.Fatalf("go vet passed on an app calling http.Get; the suite did not run:\n%s", out)
	}
	if !strings.Contains(out, "zero-timeout HTTP client: http.Get") {
		t.Fatalf("vet failed without the clienttimeout finding:\n%s", out)
	}
}

func TestVerifyVetHonoursAllowMarker(t *testing.T) {
	out, ok := runGoVet(writeVetApp(t, "//gofastr:allow(clienttimeout) probes a closed local port"), true)
	if !ok {
		t.Fatalf("an allow marker with a reason should silence the finding:\n%s", out)
	}
}

// -vettool replaces vet's default passes rather than adding to them, so
// the suite runs beside plain `go vet`, as CI runs them.
func TestVerifyVetKeepsDefaultPasses(t *testing.T) {
	dir := writeVetApp(t, "//gofastr:allow(clienttimeout) probes a closed local port")
	writeTestFile(t, filepath.Join(dir, "printf.go"), `package main

import "fmt"

func greet() { fmt.Printf("%d\n", "not a number") }
`)
	out, ok := runGoVet(dir, true)
	if ok || !strings.Contains(out, "Printf format %d has arg") {
		t.Fatalf("vet's printf pass did not run beside the suite (ok=%v):\n%s", ok, out)
	}
}
