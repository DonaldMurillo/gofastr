package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestAuditOracleDetectsSyntheticInjection proves the oracle can fail
// (CLAUDE.md hard rule 11): source where the marker escaped its literal is
// flagged, source where it stayed data is not. If this test fails, every
// "clean" verdict above is worthless.
func TestAuditOracleDetectsSyntheticInjection(t *testing.T) {
	bad := "package main\n\nvar s = `x` + PWNX() + `y`\n"
	injected, perr := clitest.AuditInjectedIdent("bad.go", bad)
	if perr != nil {
		t.Fatalf("oracle could not parse its own synthetic source: %v", perr)
	}
	if !injected {
		t.Fatal("ORACLE IS VACUOUS: synthetic breakout source was not flagged")
	}
	badInterp := "package main\n\nvar s = \"x\" + PWNX() + \"y\"\n"
	injected, _ = clitest.AuditInjectedIdent("bad2.go", badInterp)
	if !injected {
		t.Fatal("ORACLE IS VACUOUS: interpreted-string breakout not flagged")
	}
	good := "package main\n\nvar s = \"x`+PWNX()+`y\"\n"
	injected, perr = clitest.AuditInjectedIdent("good.go", good)
	if perr != nil {
		t.Fatalf("oracle broke on inert source: %v", perr)
	}
	if injected {
		t.Fatal("oracle false-positive: payload inside a quoted literal is data, not code")
	}
}
