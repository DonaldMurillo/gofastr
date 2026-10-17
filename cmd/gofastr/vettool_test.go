package main

import (
	"encoding/json"
	"errors"
	"os"
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
	out, _, ok := runGoVet(writeVetApp(t, ""), true)
	if ok {
		t.Fatalf("go vet passed on an app calling http.Get; the suite did not run:\n%s", out)
	}
	if !strings.Contains(out, "zero-timeout HTTP client: http.Get") {
		t.Fatalf("vet failed without the clienttimeout finding:\n%s", out)
	}
}

func TestVerifyVetHonoursAllowMarker(t *testing.T) {
	out, _, ok := runGoVet(writeVetApp(t, "//gofastr:allow(clienttimeout) probes a closed local port"), true)
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
	out, _, ok := runGoVet(dir, true)
	if ok || !strings.Contains(out, "Printf format %d has arg") {
		t.Fatalf("vet's printf pass did not run beside the suite (ok=%v):\n%s", ok, out)
	}
}

func TestSuiteSkipsNewerToolchain(t *testing.T) {
	for _, c := range []struct {
		module, built string
		skip          bool
	}{
		{"go1.28.0", "go1.27.2", true},
		{"go1.28rc1", "go1.27.2", true},
		{"go1.27.5", "go1.27.2", false},
		{"go1.26.1", "go1.27.2", false},
		{"", "go1.27.2", false},
		{"go1.28.0", "devel +abc", false},
	} {
		if got := suiteSkipReason(c.module, c.built) != ""; got != c.skip {
			t.Errorf("module %q built %q: skip=%v, want %v", c.module, c.built, got, c.skip)
		}
	}
}

// A tree that fails plain vet stops there: the suite pass would print
// the same type errors again.
func TestVerifyVetStopsAfterPlainVet(t *testing.T) {
	dir := writeVetApp(t, "")
	writeTestFile(t, filepath.Join(dir, "broken.go"), "package main\n\nfunc broken() { undefinedName() }\n")
	out, _, ok := runGoVet(dir, true)
	if ok {
		t.Fatal("vet passed a tree that does not type-check")
	}
	if n := strings.Count(out, "undefined: undefinedName"); n != 1 {
		t.Fatalf("type error printed %d times, want 1:\n%s", n, out)
	}
}

// Only the framework's own tree builds ./cmd/vettool. An app that has
// a directory of that name (empty here, or a tool of its own) still
// meets the suite through this binary.
func TestAppVettoolDirGetsSuite(t *testing.T) {
	dir := writeVetApp(t, "")
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "vettool"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, _, ok := runGoVet(dir, true)
	if ok || !strings.Contains(out, "zero-timeout HTTP client: http.Get") {
		t.Fatalf("an app with a cmd/vettool dir skipped the suite (ok=%v):\n%s", ok, out)
	}
}

// In the framework's own tree a vettool that does not build fails the
// stage: passing on plain vet alone would skip every analyzer.
func TestBrokenRepoVettoolFails(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module "+gofastrModule+"\n\ngo "+goVersion+"\n")
	// Type-checks, so plain vet passes; has no main, so go build fails.
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "vettool"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "cmd", "vettool", "tool.go"), "package main\n\nfunc helper() {}\n")
	out, _, ok := runGoVet(dir, true)
	if ok || !strings.Contains(out, "./cmd/vettool does not build") {
		t.Fatalf("a framework tree with a broken vettool passed verify (ok=%v):\n%s", ok, out)
	}
}

// GOFASTR_VETTOOL left in a shell must not turn `gofastr docs` into
// the analyzer driver: only vet's own calls hand off.
func TestVettoolEnvNeedsVetArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"docs"}, false},
		{[]string{"verify", "./..."}, false},
		{[]string{"-V=full"}, true},
		{[]string{"-flags"}, true},
		{[]string{"-printf.funcs=Logf", "/tmp/b001/vet.cfg"}, true},
	} {
		if got := isVetCall(c.args); got != c.want {
			t.Errorf("isVetCall(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

// An agent reading --json must learn the analyzers did not run, not
// just that vet passed.
func TestVerifyJSONSaysSuiteSkipped(t *testing.T) {
	old := cliGoVersion
	cliGoVersion = func() string { return "go1.20.0" }
	t.Cleanup(func() { cliGoVersion = old })
	out, _ := captureVerify(t, []string{"--root", writeVetApp(t, ""), "--json"})
	var doc struct {
		Vet struct {
			Passed       bool   `json:"passed"`
			SuiteSkipped string `json:"suiteSkipped"`
		} `json:"vet"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if !doc.Vet.Passed || !strings.Contains(doc.Vet.SuiteSkipped, "go1.20.0") {
		t.Fatalf("vet = %+v, want passed with the skip reason naming go1.20.0", doc.Vet)
	}
}

// With no path to itself the CLI cannot be the vettool, and a verify
// that skipped the analyzer pass for that reason must not pass.
func TestVerifyFailsWithoutSelfPath(t *testing.T) {
	old := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("no /proc") }
	t.Cleanup(func() { osExecutable = old })
	out, _ := captureVerify(t, []string{"--root", writeVetApp(t, ""), "--json"})
	var doc struct {
		Vet struct {
			Passed bool `json:"passed"`
		} `json:"vet"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	if doc.Vet.Passed {
		t.Fatalf("vet passed with the analyzer pass unable to start:\n%s", out)
	}
}
