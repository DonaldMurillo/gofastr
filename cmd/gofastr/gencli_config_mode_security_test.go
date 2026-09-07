package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// Pins: the generated CLI login config keeps owner-only mode across overwrites.
// (family enumeration; tier T2).
// Pinned sibling: TestEnvFileIsOwnerOnly / TestGeneratedEnvIsOwnerOnly
// (cmd/gofastr/init_envperm_security_test.go:23/:81) — the owner-only-on-
// overwrite family grammar, implemented by the renderer CLI's own
// init.go::writeEnvFile (:577-588: OpenFile, then f.Chmod(0o600) on the
// handle, then write — "os.WriteFile applies its mode only when CREATING
// the file"). The emitted login-config template is the divergent surface.
// Property: the generated CLI login config (bearer token, fixed name
// config.json, rewritten on every login/logout) keeps owner-only mode
// across overwrites.
// Surfaces: cmd/gofastr/generate_cli.go::renderCLIConfig → saveConfig
// template :900-917 — the credential write is os.WriteFile(path, data,
// 0o600), which applies the mode only on CREATE; a pre-existing 0644
// config.json (operator chmod, restored backup, dotfiles manager) is
// truncated and refilled with the live bearer token while world-readable.
// Finding: runLogin → saveConfig overwrites the fixed-name credential file
// with no mode enforcement, so the second login onto a loose-mode file
// persists the token world-readable — exactly the class writeEnvFile's
// comment describes, one emitter away.
// Fix direction: emit the writeEnvFile grammar in the template — OpenFile
// (O_WRONLY|O_CREATE|O_TRUNC, 0o600), f.Chmod(0o600) on the handle BEFORE
// the credential bytes land, then write.

func TestGenCLIRedConfigChmodOnWrite(t *testing.T) {
	spec, err := buildCLISpec(cliFixtureDecls(), defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("setup broken: buildCLISpec: %v", err)
	}
	src := renderCLIConfig(spec)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "config.go", src, parser.AllErrors)
	if err != nil {
		t.Fatalf("setup broken: emitted config.go does not parse: %v\n%s", err, src)
	}
	var window string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "saveConfig" || fn.Body == nil {
			continue
		}
		window = src[fn.Pos()-1 : fn.End()-1]
	}
	if window == "" {
		t.Fatalf("setup broken: no saveConfig func in emitted config.go")
	}

	// Control: the 0600 create-mode literal survives — the demanded fix is
	// mode enforcement on overwrite, not a loosened default.
	if !strings.Contains(window, "0o600") {
		t.Fatalf("setup broken: rendered saveConfig lost the 0600 create-mode literal:\n%s", window)
	}

	// Property: a Chmod on the write path (handle form is the family
	// grammar; path form accepted) so an existing loose-mode file is
	// tightened before/around the credential write.
	if !strings.Contains(window, ".Chmod(") && !strings.Contains(window, "os.Chmod(") {
		t.Errorf("SECURITY: [gencli-config-overwrite-mode] rendered saveConfig writes the bearer-token config with os.WriteFile only: the 0600 argument applies solely on CREATE, so re-login onto a pre-existing 0644 config.json truncates and refills the credential file while world-readable. The renderer CLI's own writeEnvFile (init.go:577) already implements the family grammar — open, chmod on the handle, then write. Window:\n%s", window)
	}
}
