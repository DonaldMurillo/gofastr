package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Generated-code gate: the emitted customer CLI must pass the repo's own
// vettool. The round-3 control-bytes finding
// (cmd/gofastr/clisummary_red_test.go::TestCLISummaryControlBytesRefused)
// lives entirely in GENERATED output: the fmt.Printf that prints an
// operation's summary raw sits inside the Go source template in
// generate_cli.go (printUsage/groupUsage, ~lines 795-830), so no analyzer
// over THIS repo can see the sink — the summary only becomes live bytes in
// the customer's emitted main.go/operations.go. The repo's pattern for
// emitted code is to build it (TestBlueprintHooksCompile type-checks a
// generated app); this gate vets it:
//
//	regenerate a CLI from a fixture spec into a temp module,
//	build ./cmd/vettool from the repo,
//	run go vet -vettool=<it> over the generated module,
//	fail on ANY diagnostic, printing it.
//
// Two legs, both RED while the round-3 findings are open:
//
//   - the control-bytes leg: a spec summary carrying terminal-control
//     bytes must be refused at spec build (the buildCLISpec Selection
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
//     on with empty URL/token.
//
// Deterministic and offline: the temp module replaces
// github.com/DonaldMurillo/gofastr with the repo root and pins
// GOPROXY=off; a module-resolution failure skips with a message rather
// than failing. Runtime ~3s warm (vettool build + one go vet), so it
// stays in -short.
func TestGeneratedCLIPassesRepoVettool(t *testing.T) {
	// ── leg 1: terminal-control summaries never ship live ────────────
	hostile := "list things\x1b]0;pwned\x07\r  EVIL"
	spec, err := buildOpenAPICLISpec(cliGateSpecDoc(hostile), cliGateOptions(), "example.com/app/cli/internal/client")
	if err != nil {
		// Refused at spec build is exactly the asserted fix direction
		// (the buildCLISpec Selection guard's sibling).
		t.Logf("spec build refused the control-byte summary (the fix): %v", err)
	} else {
		for _, f := range renderCLIFiles(spec) {
			if strings.Contains(f.content, "pwned") || strings.Contains(f.content, "EVIL") {
				t.Errorf("SECURITY: [cli-summary-c0] %s carries the terminal-control summary live (%q) — the shared printUsage/groupUsage scaffold prints command.summary raw, so ESC/OSC/CR from a URL-fetched spec rewrite the help text the operator reads to decide what to run; refuse control-byte summaries at spec build (next to the Selection guard) or scrub them before storage", f.name, hostile)
			}
		}
	}

	// ── leg 2: the vettool over the emitted module ────────────────────
	spec, err = buildOpenAPICLISpec(cliGateSpecDoc("list things for the demo"), cliGateOptions(), "example.com/app/cli/internal/client")
	if err != nil {
		t.Fatalf("benign fixture spec must build: %v", err)
	}
	files := renderCLIFiles(spec)
	if _, err := fileSetFromGeneratedFiles(files, "cli"); err != nil {
		t.Fatalf("generated CLI did not parse (the production emit gate): %v", err)
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
	// cmd/<binary>/ layout, the installable-main convention runGenerateCLI
	// defaults to; the clientImport above matches it.
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/app\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurio/gofastr v0.0.0\n\nreplace github.com/DonaldMurio/gofastr => "+repoRoot+"\n")
	for _, f := range files {
		full := filepath.Join(dir, "cli", f.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(f.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("no go toolchain to vet the generated module: %v", err)
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
	if err != nil {
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
}

// cliGateOptions mirrors defaultCLIOptions with the --from-openapi flag
// the spec builder reads for its header note.
func cliGateOptions() cliOptions {
	opts := defaultCLIOptions()
	opts.fromOpenAPI = "spec.json"
	return opts
}

// cliGateSpecDoc builds one fixture OpenAPI document with the given
// operation summary: a GET with query and header parameters, a POST with
// a path parameter and a JSON body, apiKey security, and a server entry —
// enough surface to exercise the self-client, operations.go, and the
// shared scaffold both generation modes emit.
func cliGateSpecDoc(summary string) map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"servers": []any{map[string]any{"url": "https://api.example.com"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"apiKey": map[string]any{"type": "apiKey", "in": "header", "name": "X-Api-Key"},
			},
		},
		"security": []any{map[string]any{"apiKey": []any{}}},
		"paths": map[string]any{
			"/things": map[string]any{
				"get": map[string]any{
					"operationId": "listThings",
					"summary":     summary,
					"parameters": []any{
						map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer"}},
						map[string]any{"name": "X-Trace", "in": "header", "schema": map[string]any{"type": "string"}},
					},
					"responses": map[string]any{"200": map[string]any{"content": map[string]any{"application/json": map[string]any{}}}},
				},
			},
			"/things/{id}": map[string]any{
				"post": map[string]any{
					"operationId": "createThing",
					"summary":     summary,
					"parameters":  []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}},
					"requestBody": map[string]any{
						"required": true,
						"content":  map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}}},
					},
					"responses": map[string]any{"201": map[string]any{}},
				},
			},
		},
	}
}
