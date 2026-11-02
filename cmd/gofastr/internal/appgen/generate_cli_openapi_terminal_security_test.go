package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// binaryBodySpec is a one-operation document whose request body is binary
// under the content type ct.
func binaryBodySpec(ct string) map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"paths": map[string]any{
			"/upload": map[string]any{
				"post": map[string]any{
					"operationId": "upload",
					"requestBody": map[string]any{
						"content": map[string]any{
							ct: map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}},
						},
					},
				},
			},
		},
	}
}

// Property: no spec-derived string reaches the generated CLI's help output
// carrying terminal-control bytes. The summary and parameter descriptions
// were refused at spec build; the binary body's content type was not, and
// it is printed raw in the --file flag's usage line ("request body file
// (%s)"), which `<cli> <op> --help` writes to the operator's terminal.
// Surfaces: oaBuildOp's binary-body branch.
func TestOpenAPICLI_BinaryContentTypeControlBytesRefused(t *testing.T) {
	for _, ct := range []string{
		"image/png\x1b]0;pwned\x07",   // OSC title set
		"application/x\u009b2J",       // 8-bit CSI: clear screen
		"application/octet-stream\r ", // CR line rewrite
		"application/x;\n",            // newline
		"not a media type",            // no type/subtype
	} {
		if _, err := BuildOpenAPICLISpec(binaryBodySpec(ct), defaultCLIOptions(), "example.com/app/internal/client"); err == nil {
			t.Errorf("content type %q: accepted; the generated --file usage would print it raw", ct)
		}
	}
	spec, err := BuildOpenAPICLISpec(binaryBodySpec("image/png"), defaultCLIOptions(), "example.com/app/internal/client")
	if err != nil {
		t.Fatalf("a plain binary content type was refused: %v", err)
	}
	if spec.Ops[0].BodyKind != "binary" || spec.Ops[0].BodyContentType != "image/png" {
		t.Fatalf("op = %+v, want a binary image/png body", spec.Ops[0])
	}
}

// Property: a refused spec's error reaches the operator as data. Under
// --dry-run --json it is one valid JSON document (printGeneratedErrorsJSON
// spelled the message with Go's %q, whose \x1b and \a are not JSON
// escapes), and in plain mode a spec string carrying control bytes is
// encoded, not written raw to the terminal running `gofastr generate`.
func TestOpenAPICLI_RefusalOutputIsData(t *testing.T) {
	hostile := map[string]any{
		"openapi": "3.1.0",
		"paths": map[string]any{
			"/things": map[string]any{
				"get": map[string]any{
					"operationId": "listThings",
					"parameters": []any{
						map[string]any{"name": "a\x1b]0;pwned\x07b", "in": "query", "schema": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
	raw, err := json.Marshal(hostile)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, dir)

	for _, asJSON := range []bool{true, false} {
		opts := defaultCLIOptions()
		opts.FromOpenAPI = specPath
		opts.dryRun, opts.json = asJSON, asJSON
		code := -1
		out := clitest.CovT_capStdout(t, func() {
			code = clitest.CovT_capExit(t, func() { runGenerateCLIFromOpenAPI(opts) })
		})
		if code != 1 {
			t.Fatalf("json=%v: exit %d, want 1; output %q", asJSON, code, out)
		}
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Errorf("json=%v: the output carries the spec's control bytes raw: %q", asJSON, out)
		}
		if asJSON {
			var doc struct {
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Errorf("--dry-run --json printed invalid JSON (%v): %q", err, out)
			} else if len(doc.Errors) != 1 || !strings.Contains(doc.Errors[0].Message, "pwned") {
				t.Errorf("--dry-run --json errors = %+v, want the one refusal", doc.Errors)
			}
		}
	}
}
