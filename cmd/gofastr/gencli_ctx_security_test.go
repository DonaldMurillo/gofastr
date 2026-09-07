package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// Pins: every scaffolded verb runs under a cancellable context.
// (family enumeration; tier T3).
// Family: scaffolded CLIs inherit their context hygiene from the emitter —
// whatever parseGlobals renders is multiplied to every downstream project
// the scaffold touches.
// Pinned sibling (green today, same emitted scaffold): the Watch template
// (generate_cli.go:1481) wires signal.NotifyContext(g.ctx, os.Interrupt)
// itself; the repo's operative-deadline convention is
// TestBootProbeClientTimesOut (cmd/gofastr/examples_blueprint_test.go:601).
// Property: every scaffolded verb runs under a cancellable context — the
// shared parseGlobals must hand verbs a ctx that can be cancelled or
// deadline-bound, not context.Background().
// Surfaces: cmd/gofastr/generate_cli.go::renderCLIOutput parseGlobals
// template :1077 — `return &global{ctx: context.Background(), client: c}, 0`.
// Only the Watch template installs signal.NotifyContext (:1481); every
// other verb (list/get/create/update/patch/delete/batch-*) inherits the
// Background ctx, so Ctrl-C falls through to raw process death mid-request
// and no verb ever sees ctx cancellation.
// Finding: source-asserted below — parseGlobals in the rendered scaffold
// contains neither signal.NotifyContext nor context.WithTimeout while the
// Watch verb in the same scaffold proves the emitter knows the pattern.
// Fix direction: build the ctx in parseGlobals via signal.NotifyContext
// (every verb becomes interruptible; Watch's own wiring then collapses
// onto it), or wrap every verb runner's call in context.WithTimeout.

func TestGenCLIRedCtxCancellable(t *testing.T) {
	spec, err := buildCLISpec(cliFixtureDecls(), defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatalf("setup broken: buildCLISpec: %v", err)
	}
	outSrc := renderCLIOutput(spec)

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

	// Control: parseGlobals is still in the context business — the finding
	// is that the ctx it builds is uncancellable, not that it vanished.
	if !strings.Contains(window, "context.") {
		t.Fatalf("setup broken: emitted parseGlobals no longer constructs the global ctx:\n%s", window)
	}
	// Control: the scaffold still emits non-Watch verbs — only Watch being
	// cancellable is exactly the gap being pinned.
	entSrc := renderCLIEntityFile(spec, spec.Entities[0])
	if !strings.Contains(entSrc, "run"+spec.Entities[0].Struct+"List") {
		t.Fatalf("setup broken: fixture entity lost its non-watch verbs — the Background-ctx finding needs verbs other than watch")
	}

	if !strings.Contains(window, "signal.NotifyContext") && !strings.Contains(window, "context.WithTimeout") {
		t.Errorf("SECURITY: [gencli-ctx-background] rendered parseGlobals hands every scaffolded verb a ctx built from context.Background() (generate_cli.go:1077) — only Watch installs signal.NotifyContext (:1481), so every other verb (list/get/create/update/patch/delete/batch-*) ignores Ctrl-C until the transport gives up, and the scaffold multiplies that to every downstream project. parseGlobals must return a NotifyContext-built ctx, or every verb runner gets a context.WithTimeout wrapper. Window:\n%s", window)
	}
}
