package main

import (
	"strings"
	"testing"
)

// Pins the 8-bit C1 hole in the OpenAPI→CLI summary guard, found by the
// 2026-09-05 red-probe round (round 4); fixed by widening
// generate_cli_openapi.go's ContainsFunc with textsafe.IsC1
// (U+0080–U+009F), keeping \n and \t allowed.
// Family: F25 Bidi, invisible, and confusable characters
// Property: an OpenAPI operation summary carrying a terminal-control byte
// in its 8-bit C1 form (U+0080..U+009F, e.g. U+009B CSI / OSC introducer)
// must be refused at spec build, exactly like the C0/DEL bytes the guard
// at generate_cli_openapi.go already rejects — printUsage prints the
// summary verbatim into the operator's terminal.
// Surfaces: cmd/gofastr/generate_cli_openapi.go's summary guard, the
// choke point for both URL-fetched and repo-committed specs; the emitted
// help text is the sink (shared printUsage/groupUsage scaffold).
// Threat: U+009B is above 0x20 and is not 0x7f, so the 8-bit CSI
// sequence reached the terminal the same way its ESC-prefixed twin
// (already rejected) would; not all emulators interpret it, but the
// guard exists precisely to be complete about it.
func TestOASummaryRefusesC1Controls(t *testing.T) {
	doc := cliGateSpecDoc("list things\u009b31;31m EVIL — title\u009b]0;pwned")
	_, err := buildOpenAPICLISpec(doc, cliGateOptions(), "example.com/m/cli/internal/client")
	if err == nil {
		t.Error("SECURITY: [oa-summary-c1] spec with a summary carrying the 8-bit C1 CSI/OSC form (U+009B) was accepted; printUsage prints it verbatim to the terminal")
	}
	if err != nil && strings.Contains(err.Error(), "operationId") {
		t.Fatalf("fixture rejected for the wrong reason (identifier, not summary): %v", err)
	}
}
