package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// Pins: the scaffolded CLI registers no --token string flag — a credential never sits in argv.
// (family enumeration; tier T3 CONTRACT-QUESTION).
// CONTRACT-QUESTION: same grade as the pinned harness-creds and a11y argv
// reds (cli_credstdin_red_test.go, audit_password_red_test.go —
// candidates #53/#54): the --token flag is part of the scaffold's
// documented interface (its own usage string), so dropping it is a UX
// call for the maintainer. Delete this test if argv entry stays.
// Pinned sibling: TestHarnessCredsRedStdinSecret
// (cli_credstdin_red_test.go) and TestAuditA11yRedPasswordEnv
// (audit_password_red_test.go) — the credentials-never-in-argv family
// (harness-architecture.md:1061 documents the doctrine); the generated
// CLI scaffold is the third surface of that same property.
// Property: a credential never sits in argv — the scaffolded CLI must not
// register a --token <value> string flag on every verb while complete
// argv-free channels exist.
// Surfaces: cmd/gofastr/generate_cli.go::renderCLIOutput parseGlobals
// :1060 — `tokenF := fs.String("token", "", "API token (default
// $"+envPrefix+"_TOKEN, then stored config)")` emitted onto every
// scaffolded verb.
// Finding: every generated verb accepts the bearer token as a literal argv
// value, so `myapp posts list --token gfsk_...` parks the credential in
// world-readable process state (ps/procfs) for the whole call — and the
// scaffold multiplies the surface to every downstream project it renders.
// Fix direction: stop registering the string flag; resolution order
// becomes env > stored config (both already implemented in the same
// window), and login --with-token stdin remains the mint path.

func TestGenCLIRedNoTokenFlag(t *testing.T) {
	spec, err := buildCLISpec(cliFixtureDecls(), defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("setup broken: buildCLISpec: %v", err)
	}
	outSrc := renderCLIOutput(spec)
	authSrc := renderCLIAuth(spec)

	// Controls: the argv-free channels survive in the rendered sources —
	// env resolution inside parseGlobals, and the login --with-token stdin
	// path — so the demanded refusal is never vacuous.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "output.go", outSrc, parser.AllErrors)
	if err != nil {
		t.Fatalf("setup broken: emitted output.go does not parse: %v\n%s", err, outSrc)
	}
	var window string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "parseGlobals" || fn.Body == nil {
			continue
		}
		window = outSrc[fn.Pos()-1 : fn.End()-1]
	}
	if window == "" {
		t.Fatalf("setup broken: no parseGlobals func in emitted output.go")
	}
	if !strings.Contains(window, `os.Getenv(envPrefix+"_TOKEN")`) {
		t.Fatalf("setup broken: emitted parseGlobals lost the env token resolution:\n%s", window)
	}
	if !strings.Contains(authSrc, `fs.Bool("with-token"`) || !strings.Contains(authSrc, "io.ReadAll(os.Stdin)") {
		t.Fatalf("setup broken: emitted login lost the --with-token stdin path")
	}

	// Property: no string "token" flag registration, unless the emitted
	// code carries an explicit refusal of a supplied value (a guard on
	// *tokenF != "" that rejects argv entry counts as the fix, not the
	// finding).
	if strings.Contains(window, `fs.String("token"`) && !strings.Contains(window, `*tokenF != ""`) {
		t.Errorf("SECURITY: [gencli-token-argv] rendered parseGlobals registers fs.String(\"token\", ...) on every scaffolded verb: the bearer credential is accepted as a literal argv value and sits in ps/procfs world-readable state for the whole call. The argv-free channels already exist in the same rendered source (the envPrefix+_TOKEN env resolution and `login --with-token` stdin); the flag must go, or explicitly refuse a supplied value. Window:\n%s", window)
	}
}
