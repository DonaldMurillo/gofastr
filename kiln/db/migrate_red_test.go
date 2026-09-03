//go:build red

// RED TEST — open finding, 2026-09-03 round 3 (tests-only; no fix applied).
// Property: identifiers spliced into kiln's schema-sync SQL must pass the
// same SafeIdent validation every framework sibling applies before
// interpolating (core/query.SafeIdent). The framework's own live-column
// read for the identical PRAGMA validates and is pinned
// (framework/migrate/migrate_security_test.go
// TestReadLiveColumnsRejectsBadTable); kiln/db keeps a private mirror of
// that read which interpolates with no check at all.
// Surfaces: kiln/db/migrate.go tableColumns — `PRAGMA table_info(` + table
// + `)` at migrate.go:84; kiln/db/migrate.go alterAddColumn —
// `ALTER TABLE %s ADD COLUMN %s` with table and f.Name at migrate.go:104,112;
// both reached per-entity from alignColumns (migrate.go:66-80) via
// db.Migrate.
// Finding: a metachar-carrying table or field name produces no validation
// error; the bytes are spliced verbatim into SQLite text the repo's driver
// (modernc.org/sqlite via sqlite/stdlib) executes as a multi-statement
// string once the first statement parses (proven by the ATTACH artifact in
// TestAlterAddColumnEscapesHostileDefault). Reachable chain, kiln half:
// add_entity accepts a `table` override and hostile field names over HTTP,
// kiln/journal/replay.go:202-217 validates only the entity Name,
// kiln/render/render.go:190 copies Table verbatim into the rebuild
// declaration, kiln/live/live.go:214 runs db.Migrate on it. No SafeIdent
// anywhere between HTTP and the PRAGMA/ALTER. Severity: high — the unpinned
// twin of a pinned, fixed framework property.
// Fix direction: validate table and f.Name via core/query.SafeIdent before
// splicing — tableColumns and alignColumns return the SafeIdent error
// (exactly what ReadLiveColumnsSQLite does for the twin PRAGMA); alignColumns
// must check f.Name too, since tableColumns' check cannot see field names.
package db

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestKilnMigrateRedValidatesIdentifiers(t *testing.T) {
	d, cleanup, err := EphemeralSQLite("kiln-red-ident")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := d.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`CREATE TABLE kiln_red_pragma_secrets (token TEXT)`); err != nil {
		t.Fatal(err)
	}

	tableExists := func(name string) bool {
		var one int
		return d.QueryRow(`SELECT 1 FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&one) == nil
	}

	// tableColumns: the hostile table reaches PRAGMA table_info unvalidated.
	for _, bad := range []string{
		`victims); DROP TABLE kiln_red_pragma_secrets; --`,
		"victims\nAND 1=1",
	} {
		_, err := tableColumns(d, bad)
		if err == nil {
			t.Errorf("SECURITY: [kiln/db] tableColumns accepted table name %q (err == nil, "+
				"decoy table dropped by the injected tail: %v). Attack: an agent-supplied "+
				"table override reaches PRAGMA table_info unvalidated (replay validates "+
				"only the entity Name), the unpinned twin of ReadLiveColumnsSQLite's "+
				"pinned check.", bad, !tableExists("kiln_red_pragma_secrets"))
			continue
		}
		if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "invalid") {
			t.Errorf("[kiln/db] tableColumns rejected %q only incidentally (%v): no "+
				"identifier validation happens before the splice.", bad, err)
		}
	}

	// alignColumns: a hostile FIELD name tableColumns cannot see, spliced
	// into ALTER TABLE ... ADD COLUMN. The first statement parses (SQLite
	// allows a type-less added column), so the driver runs the DROP tail.
	hostile := &framework.Entity{Config: framework.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: `evil; DROP TABLE kiln_red_alter_secrets; --`, Type: schema.String},
		},
	}}
	if _, err := d.Exec(`CREATE TABLE kiln_red_alter_secrets (token TEXT)`); err != nil {
		t.Fatal(err)
	}
	err = alignColumns(d, hostile)
	if err == nil {
		t.Errorf("SECURITY: [kiln/db] alignColumns accepted field name %q (err == nil, "+
			"decoy table dropped by the injected tail: %v); the name is spliced "+
			"verbatim into ALTER TABLE ... ADD COLUMN and the driver executes the "+
			"multi-statement tail. tableColumns validates nothing and nothing else "+
			"checks field names.",
			hostile.Config.Fields[0].Name, !tableExists("kiln_red_alter_secrets"))
	} else if !strings.Contains(err.Error(), "unsafe") && !strings.Contains(err.Error(), "invalid") {
		t.Errorf("[kiln/db] alignColumns rejected the hostile field only incidentally "+
			"(%v): no identifier validation happens before the ALTER splice.", err)
	}

	// Control: plain identifiers must keep syncing, the fix cannot be
	// "reject everything".
	if err := alignColumns(d, &framework.Entity{Config: framework.EntityConfig{
		Name:  "posts",
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String}, // already present
			{Name: "body", Type: schema.Text},    // genuinely new
		},
	}}); err != nil {
		t.Errorf("[kiln/db] alignColumns regressed on benign identifiers: %v", err)
	}
}
