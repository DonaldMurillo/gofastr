package generatecmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// Generated-code gate: the emitted customer CLI must pass the repo's own
// vettool. The round-3 control-bytes finding
// (cmd/gofastr/internal/appgen/generate_cli_injection_security_test.go::TestCLISummaryControlBytesRefused)
// lives entirely in GENERATED output: the fmt.Printf that prints an
// operation's summary raw sits inside the Go source template in
// generate_cli.go (printUsage/groupUsage, ~lines 795-830), so no analyzer
// over THIS repo can see the sink — the summary only becomes live bytes in
// the customer's emitted main.go/operations.go. The repo's pattern for
// emitted code is to build it (appgen's TestUnionApp type-checks a
// generated app); this gate vets it:
//
//	regenerate a CLI from a fixture spec into a temp module,
//	share the repo's ./cmd/vettool build (clitest.RepoVettool),
//	run go vet -vettool=<it> over the generated module,
//	fail on ANY diagnostic, printing it.
//
// Three legs:
//
//   - the control-bytes leg: a spec summary carrying terminal-control
//     bytes must be refused at spec build (the buildCLISpec --api-prefix
//     guard's sibling) or scrubbed — never emitted live where the shared
//     printUsage scaffold prints it to the operator's terminal. Today the
//     hostile summary lands verbatim in operations.go, so this leg fails.
//     No vettool analyzer fires on that sink (controlbytes' struct seam
//     only widens at message sinks, and the command struct is filled from
//     literals), which is exactly why this leg checks the bytes directly;
//   - the vettool leg: the generated module must produce zero diagnostics
//     under the repo vettool. Today discardeddecode fires on the emitted
//     config.go's `_ = json.Unmarshal` (renderCLIConfig), a real finding:
//     a corrupt config file parses to the zero value and the CLI marches
//     on with empty URL/token;
//   - the entity-path leg (vetEntityPathGeneratedCLI): the same vettool
//     over an entity-mode module, which additionally carries verbs.go and
//     the per-entity flag/field tables.
//
// Deterministic and offline: the temp modules replace
// github.com/DonaldMurillo/gofastr with the repo root and pin
// GOPROXY=off; a module-resolution failure skips with a message rather
// than failing. Runtime ~15s warm (vettool build, module scaffolds, three
// go vets).
//
// The vettool build is a subprocess no import edge shows: an analyzer
// change selects this package through the directive below.
//
//gofastr:test-reads internal/analyzers/ cmd/vettool/
func TestGeneratedCLIPassesRepoVettool(t *testing.T) {
	// ── leg 1: terminal-control summaries never ship live ────────────
	hostile := "list things\x1b]0;pwned\x07\r  EVIL"
	spec, err := appgen.BuildOpenAPICLISpec(clitest.CliGateSpecDoc(hostile), cliGateOptions(), "example.com/app/cli/internal/client")
	if err != nil {
		// Refused at spec build is exactly the asserted fix direction
		// (the buildCLISpec --api-prefix guard's sibling).
		t.Logf("spec build refused the control-byte summary (the fix): %v", err)
	} else {
		for _, f := range appgen.RenderCLIFiles(spec) {
			if strings.Contains(f.Content, "pwned") || strings.Contains(f.Content, "EVIL") {
				t.Errorf("SECURITY: [cli-summary-c0] %s carries the terminal-control summary live (%q) — the shared printUsage/groupUsage scaffold prints command.summary raw, so ESC/OSC/CR from a URL-fetched spec rewrite the help text the operator reads to decide what to run; refuse control-byte summaries at spec build (next to the --api-prefix guard) or scrub them before storage", f.Name, hostile)
			}
		}
	}

	// ── leg 2: the vettool over the emitted module ────────────────────
	//
	// The --from-openapi module is vetted WHOLE, and that must include
	// the self-contained client (renderCLISelfClient, emitted as
	// internal/client/client.go because buildOpenAPICLISpec forces
	// SelfClient): every operation funnels through its DoRaw, so a
	// finding there is in the customer's emitted code, exactly the
	// class this gate exists for. Assert the file is present so the
	// fixture or generator cannot silently drop the leg.
	spec, err = appgen.BuildOpenAPICLISpec(clitest.CliGateSpecDoc("list things for the demo"), cliGateOptions(), "example.com/app/cli/internal/client")
	if err != nil {
		t.Fatalf("benign fixture spec must build: %v", err)
	}
	files := appgen.RenderCLIFiles(spec)
	selfClient := false
	for _, f := range files {
		if f.Name == "internal/client/client.gen.go" {
			selfClient = true
		}
	}
	if !selfClient {
		t.Fatalf("generated CLI lost internal/client/client.go (renderCLISelfClient) — the vetgate would silently stop covering the self-contained client; fix the fixture or the emitter, not this gate")
	}
	if _, err := appgen.FileSetFromGeneratedFiles(files, "cli"); err != nil {
		t.Fatalf("generated CLI did not parse (the production emit gate): %v", err)
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// cmd/<binary>/ layout, the installable-main convention runGenerateCLI
	// defaults to; the clientImport above matches it.
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	for _, f := range files {
		full := filepath.Join(dir, "cli", f.Name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain to vet the generated module: %v", err)
	}
	vettool, err := clitest.RepoVettool()
	if err != nil {
		t.Fatal(err)
	}

	vet := exec.Command("go", "vet", "-vettool="+vettool, "./...")
	vet.Dir = dir
	vet.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
	if out, err := vet.CombinedOutput(); err != nil {
		text := string(out)
		// Offline skip: the temp module resolves the repo through the
		// replace above, so this only triggers when the environment
		// cannot build Go code at all.
		for _, marker := range []string{"cannot find module", "finding module for package", "dial tcp", "module lookup disabled"} {
			if strings.Contains(text, marker) {
				t.Skipf("generated module could not be built in this environment: %s", text)
			}
		}
		t.Errorf("GATE: generated CLI does not pass the repo vettool — every diagnostic is a finding in the emitted templates (generate_cli*.go), not in the customer's code:\n%s", text)
	}

	// ── leg 3: the entity-path module ──────────────────────────────────
	vetEntityPathGeneratedCLI(t, vettool)
}

// vetEntityPathGeneratedCLI is the entity-path leg of the vetgate: legs 1
// and 2 exercise the --from-openapi render, whose operations.go shares
// only the scaffold; the entity mode additionally emits verbs.go (the
// shared verb bodies behind the per-entity flag/field tables) and the
// per-entity files. It vets the full verb set and a create/update/patch-
// only selection in one module: the narrow one is the import-shrink
// contract (an entity file with no imports at all), so a stray reference
// there is a compile error this leg reports instead of a broken emit.
func vetEntityPathGeneratedCLI(t *testing.T, vettool string) {
	t.Helper()
	dir := cliModuleFromYAML(t, cliMutationFixtureYAML)
	appgen.RunGenerateCLI([]string{"--binary=myapp"})
	appgen.RunGenerateCLI([]string{"--binary=mymut", "--out=cmd/mymut", "--verbs=create,update,patch"})
	for _, pkg := range []string{"./cmd/myapp", "./cmd/mymut"} {
		vet := exec.Command("go", "vet", "-vettool="+vettool, pkg)
		vet.Dir = dir
		vet.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off")
		out, err := vet.CombinedOutput()
		if err == nil {
			continue
		}
		text := string(out)
		for _, marker := range []string{"cannot find module", "finding module for package", "dial tcp", "module lookup disabled"} {
			if strings.Contains(text, marker) {
				t.Skipf("generated module could not be built in this environment: %s", text)
			}
		}
		t.Errorf("GATE: entity-path generated CLI (%s) does not pass the repo vettool — every diagnostic is a finding in the emitted templates (generate_cli*.go), not in the customer's code:\n%s", pkg, text)
	}
}

// cliGateOptions mirrors defaultCLIOptions with the --from-openapi flag
// the spec builder reads for its header note.
func cliGateOptions() appgen.CliOptions {
	opts := defaultCLIOptions()
	opts.FromOpenAPI = "spec.json"
	return opts
}
