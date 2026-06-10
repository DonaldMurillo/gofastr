// SECURITY_FINDINGS.md ledger gate.
//
// The ledger header claims a finding count ("N verified findings") and
// every finding row carries a status. Both have drifted before (the
// header said 101 while the tables held 103 rows). This gate makes the
// drift impossible to commit: the header count must equal the number of
// table rows, and every row must carry a recognized status token.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

var (
	ledgerHeaderRe = regexp.MustCompile(`(\d+) verified findings`)
	ledgerRowRe    = regexp.MustCompile(`(?m)^\|\s*(\d+)\s*\|.*\|\s*(\S+)\s*\|$`)
)

func readLedger(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootDir(t), "SECURITY_FINDINGS.md"))
	if err != nil {
		t.Fatalf("read SECURITY_FINDINGS.md: %v", err)
	}
	return string(raw)
}

func TestLedgerCountMatchesRows(t *testing.T) {
	src := readLedger(t)

	m := ledgerHeaderRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal(`header claim "N verified findings" not found — keep the count line, the gate parses it`)
	}
	claimed, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse header count %q: %v", m[1], err)
	}

	rows := ledgerRowRe.FindAllStringSubmatch(src, -1)
	if len(rows) != claimed {
		t.Fatalf("header claims %d findings but the tables hold %d numbered rows — update the header count in SECURITY_FINDINGS.md to match", claimed, len(rows))
	}

	// Finding numbers must be unique — a duplicated row would silently
	// inflate the count while hiding a finding.
	seen := make(map[string]bool, len(rows))
	for _, r := range rows {
		if seen[r[1]] {
			t.Errorf("finding #%s appears more than once", r[1])
		}
		seen[r[1]] = true
	}
}

func TestLedgerRowsCarryKnownStatus(t *testing.T) {
	src := readLedger(t)

	valid := map[string]bool{"fixed": true, "open": true, "needs-verification": true, "accepted": true}
	for _, r := range ledgerRowRe.FindAllStringSubmatch(src, -1) {
		if !valid[r[2]] {
			t.Errorf("finding #%s has status %q — must be one of fixed/open/needs-verification/accepted (see the Status legend)", r[1], r[2])
		}
	}
}
