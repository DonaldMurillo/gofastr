package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// The declaration-derived strings the generators write into COMMENTS (a
// move's From/To summary in the Go client's doc comment, the same text in
// the d.ts /** … */ block) must stay inside the comment. Two probes:
//
//   - a newline in an enum value ends a `//` comment, and the rest of the
//     value lands at statement position in emitted Go that compiles and
//     runs (the trailing // in the payload keeps the leftover prose inert,
//     so nothing but the guard catches it);
//   - a `*/` in an enum value closes a /** … */ block comment early and
//     the rest of the value becomes live .d.ts tokens.
//
// The transition Label carries the same payloads: no cmd/gofastr emitter
// puts a Label in a comment today, but the scan below covers every
// emitted file, so a future emitter that does must route it through the
// same helper or this test fails.
func TestGeneratedCommentsKeepDeclarationDataInert(t *testing.T) {
	const goProbe = "paid\nfunc init() { panic(\"pwned-go\") } //"
	const closeProbe = "void*/"
	decls := []framework.EntityDeclaration{{
		Name:  "invoices",
		Table: "invoices",
		Fields: []framework.FieldDeclaration{
			{Name: "number", Type: "string", Required: true},
			{Name: "status", Type: "enum", Values: []string{"draft", "open", goProbe, closeProbe}, Default: "draft"},
			{Name: "paid_on", Type: "date"},
		},
		States: &framework.StatesConfig{
			Field:   "status",
			Initial: []string{"draft"},
			Transitions: []framework.Transition{
				{Key: "pay", Label: "Pay\nfunc init() { panic(\"pwned-label\") } //", From: []string{"open"}, To: goProbe, Stamp: "paid_on"},
				{Key: "close", From: []string{"open"}, To: closeProbe},
			},
		},
	}}

	// The payload must survive as TEXT (a sanitizer that dropped it would
	// pass every "not live code" assertion below for the wrong reason).
	clientSrc := renderClient(decls)
	if !strings.Contains(clientSrc, `func init() { panic("pwned-go") }`) {
		t.Fatal("client source lost the probe payload; the fixture no longer exercises the comment slot")
	}
	assertNoLivePayload(t, "client.go", clientSrc)

	// The emitted client must compile: sanitizing cannot just corrupt the
	// file into something gofmt would reject.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain: %v", err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module probe\n\ngo "+goVersion+"\n")
	if err := os.MkdirAll(filepath.Join(dir, "client"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "client", "client.go"), clientSrc)
	build := exec.Command("go", "build", "./...")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("sanitized client does not compile:\n%s", out)
	}

	// The CLI re-emits the same declaration strings; every emitted Go file
	// gets the same scan.
	spec, err := buildCLISpec(decls, cliOptions{binary: "myapp"}, "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("buildCLISpec: %v", err)
	}
	for _, f := range renderCLIFiles(spec) {
		assertNoLivePayload(t, f.name, f.content)
	}

	// The d.ts block comment must not be closable by the value: the `*/`
	// arrives broken, and the member declaration the comment documents
	// still follows it.
	opts := sdkOptions{name: "myapp"}
	sdkSpec, err := buildSDKSpec(decls, &opts)
	if err != nil {
		t.Fatalf("buildSDKSpec: %v", err)
	}
	for _, f := range renderSDKJSFiles(sdkSpec) {
		assertNoLivePayload(t, f.name, f.content)
		if f.name == "client.d.ts" {
			// The type union legitimately quotes the raw value ("void*/")
			// inside a %q literal; only the MOVES doc comment must carry it
			// broken, so the check there is per-line (assertNoLivePayload
			// already fails any /** line a */ closes mid-line).
			if !strings.Contains(f.content, "void* /") {
				t.Errorf("d.ts dropped the closing-probe value instead of breaking it; the comment slot no longer carries it:\n%s", f.content)
			}
			if !strings.Contains(f.content, "close(id: string): Promise<Invoices>;") {
				t.Errorf("d.ts lost the move member its comment documents; the block comment was closed early:\n%s", f.content)
			}
		}
	}
}

// assertNoLivePayload fails when a line of src carries the probe function
// at statement position, or closes a /** … */ block comment mid-line. The
// payloads end in // so an UNSANITIZED emission still parses (the leftover
// prose after the function is commented out) and the payload also travels
// LEGITIMATELY inside %q string literals as a \n escape — so a line is
// live only when the function starts it, and a block comment is closed
// early only when a */ precedes its final two characters.
func assertNoLivePayload(t *testing.T, name, src string) {
	t.Helper()
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func init()") {
			t.Errorf("%s: payload escaped the comment and compiles as live code:\n%s", name, line)
		}
		if strings.HasPrefix(trimmed, "/**") {
			if i := strings.Index(trimmed, "*/"); i >= 0 && i != len(trimmed)-2 {
				t.Errorf("%s: block comment closed early by declaration data:\n%s", name, line)
			}
		}
	}
}
