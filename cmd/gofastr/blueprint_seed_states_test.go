package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// seedsStatesYAML is an app whose invoices entity declares enforced states
// and whose seed writes a row already at a non-initial state: the seed
// runner writes under the audited state override, so this must generate,
// boot and seed cleanly.
const seedsStatesYAML = `app:
  name: SeedStates
  module: example.com/seedstates
  db:
    driver: sqlite
    url: file:gofastr.db
entities:
  - name: invoices
    crud: true
    fields:
      - name: number
        type: string
        required: true
      - name: status
        type: enum
        values: [draft, open, paid]
        default: draft
      - name: paid_on
        type: date
    states:
      field: status
      initial: [draft]
      transitions:
        - key: send
          from: [draft]
          to: open
        - key: mark_paid
          from: [open]
          to: paid
          stamp: paid_on
screens:
  - name: invoices
    route: /invoices
    body:
      - kind: entity_list
        entity: invoices
        fields: [number, status]
        create: true
seed:
  - entity: invoices
    rows:
      - number: INV-0001
        status: draft
      - number: INV-0002
        status: paid
        paid_on: "2026-01-15"
`

// TestSeededStatesBootAndAuditOverride: the generated app boots with a
// states entity seeded at a non-initial state, and the override that let
// the seed write it leaves the audit trail the escape hatch promises: a
// row per seeded row carrying the override's reason ("seed"), with the
// non-initial state recorded in the create diff.
func TestSeededStatesBootAndAuditOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, compiles and boots an app; skipped under -short")
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
	goMod := "module example.com/seedstates\n\ngo " + goVersion +
		"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatal(err)
	}
	bp, err := decodeBlueprintString(seedsStatesYAML)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, file := range mustRenderBlueprintFiles(t, bp) {
		full := filepath.Join(dir, file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		perm := file.mode
		if perm == 0 {
			perm = 0o644
		}
		if err := os.WriteFile(full, []byte(file.content), perm); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("go", "run", "-mod=mod", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PORT=127.0.0.1:0")
	configureTestProcessGroup(cmd)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = killTestProcessTree(cmd)
		_, _ = cmd.Process.Wait()
	})

	// Wait for the server banner: the seed hook ran before OnReady, so by
	// the time the port is bound every seeded row and its audit trail are
	// committed (or the app died at boot, which the log will show).
	ready := false
	for range 120 {
		if strings.Contains(out.String(), "Server running at") {
			ready = true
			break
		}
		if cmd.ProcessState != nil {
			t.Fatalf("app exited before serving:\n%s", out.String())
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("app never reported ready:\n%s", out.String())
	}

	db, err := sql.Open("sqlite3", filepath.Join(dir, "gofastr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE number = 'INV-0002' AND status = 'paid'`).Scan(&seeded); err != nil {
		t.Fatalf("query seeded invoice: %v\napp log:\n%s", err, out.String())
	}
	if seeded != 1 {
		t.Fatalf("the paid seed row did not land (found %d)", seeded)
	}
	// The override's audit trail: every seeded row wrote an audit row
	// carrying the override's reason, and the row created at a
	// non-initial state recorded that state in its diff.
	var reasons int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE entity = 'invoices' AND reason = 'seed'`).Scan(&reasons); err != nil {
		t.Fatalf("query audit rows: %v", err)
	}
	if reasons != 2 {
		t.Fatalf("want an audit row with reason seed per seeded row, got %d", reasons)
	}
	var paidDiff int
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log a JOIN invoices i ON a.record_id = i.id
		WHERE i.number = 'INV-0002' AND a.op = 'create' AND a.diff LIKE '%"status":"paid"%'`).Scan(&paidDiff); err != nil {
		t.Fatalf("query paid create diff: %v", err)
	}
	if paidDiff != 1 {
		t.Fatalf("the non-initial seed state must be recorded in its audit diff, got %d rows", paidDiff)
	}
}
