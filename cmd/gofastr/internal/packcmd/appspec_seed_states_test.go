package packcmd

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// bootPackTree boots the package's shared app (packTree), whose invoices
// entity declares enforced states and whose seed writes an invoice already
// at a non-initial state (the seed runner writes it under the audited
// state override) and a product whose currency references a declared key.
// It runs the app on a database of its own until it serves (the seed hook
// runs before that), and returns that database's path and the log so far.
func bootPackTree(t *testing.T) (string, func() string) {
	t.Helper()
	if testing.Short() {
		t.Skip("generates, compiles and boots an app; skipped under -short")
	}
	clitest.VariantBuild(t)
	dir, bin := packTree(t)
	dbPath := filepath.Join(t.TempDir(), "gofastr.db")

	cmd := exec.Command(bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PORT=127.0.0.1:0", "DATABASE_URL=file:"+dbPath)
	clitest.ConfigureTestProcessGroup(cmd)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = clitest.KillTestProcessTree(cmd)
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
	return dbPath, out.String
}

// TestSeededStatesBootAndAuditOverride: the generated app boots with a
// states entity seeded at a non-initial state, and the override that let
// the seed write it leaves the audit trail the escape hatch promises: a
// row per seeded row carrying the override's reason ("seed"), with the
// non-initial state recorded in the create diff.
func TestSeededStatesBootAndAuditOverride(t *testing.T) {
	dbPath, log := bootPackTree(t)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var seeded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE number = 'INV-0002' AND status = 'paid'`).Scan(&seeded); err != nil {
		t.Fatalf("query seeded invoice: %v\napp log:\n%s", err, log())
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

// seedRefKeyEntities are the entities of the declared-key seed reference:
// currencies keyed on currency_code, and products whose currency relation
// a seed row fills with "@currencies.label=Euro" (packTreeYAML).
const seedRefKeyEntities = `  - name: currencies
    crud: true
    primary_key: [currency_code]
    fields:
      - name: currency_code
        type: string
        required: true
      - name: label
        type: string
  - name: products
    crud: true
    fields:
      - name: name
        type: string
      - name: currency
        type: relation
        to: currencies
`

// A seed reference to an entity keyed on another column than id
// resolves to that column's value.
func TestSeedRefToDeclaredKey(t *testing.T) {
	dbPath, log := bootPackTree(t)
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var currency string
	if err := db.QueryRow(`SELECT currency FROM products WHERE name = 'Lamp'`).Scan(&currency); err != nil {
		t.Fatalf("query seeded product: %v\napp log:\n%s", err, log())
	}
	if currency != "EUR" {
		t.Errorf("product currency = %q, want EUR", currency)
	}
}
