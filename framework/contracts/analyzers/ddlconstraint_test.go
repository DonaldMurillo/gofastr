package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1416 exists because battery/auth's EntitySessionStore declared
// sessions.token UNIQUE inside a CREATE TABLE IF NOT EXISTS that never
// ran once AutoMigrate had created the entity's table (0550be96d), and
// the fix's standalone unique index then sat beside the inline UNIQUE
// and the entity field's Unique flag: two unique indexes on one column
// (dbe11ba19). The fixtures reduce EnsureSchema under other names, the
// table spelled through a quoting helper and the index's ON clause
// through a local closure over query.IndexOn, as the store spells them.

func hits1416(ds []contracts.Diagnostic) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, d := range ds {
		if d.RuleID == contracts.RuleDDLConstraint {
			out = append(out, d)
		}
	}
	return out
}

const dcStore = `package vault

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

type DB interface{ Exec(string, ...any) }

type TicketStore struct {
	db    DB
	table string
}

func NewTicketStore(db DB, table string) *TicketStore { return &TicketStore{db: db, table: table} }

func (s *TicketStore) EnsureSchema(sqlite bool) {
	s.db.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT NOT NULL, ticket TEXT TICKETDEF NOT NULL, owner_id TEXT NOT NULL, expires_at BIGINT NOT NULL)", query.QuoteFolded(s.table)))
	on := func(suffix string) string {
		return query.IndexOn(sqlite, query.QuoteFolded("idx_"+s.table+suffix), query.QuoteFolded(s.table))
	}
	for _, idx := range []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s (expires_at)", on("_expires")),
		INDEXES
	} {
		s.db.Exec(idx)
	}
}

func TicketEntityFields() []schema.Field {
	return []schema.Field{
		{Name: "ticket", Type: schema.String, Required: true FIELDFLAG},
		{Name: "owner_id", Type: schema.String, Required: true},
		{Name: "expires_at", Type: schema.Timestamp, Required: true},
	}
}
`

func dcFixture(t *testing.T, ticketDef, indexes, fieldFlag string) []contracts.Diagnostic {
	t.Helper()
	src := strings.NewReplacer("TICKETDEF", ticketDef, "INDEXES", indexes, "FIELDFLAG", fieldFlag).Replace(dcStore)
	return hits1416(fixture(t, map[string]string{"battery/vault/tickets.go": src}))
}

const dcUniqueIndex = `fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s (ticket)", on("_ticket")),`

// 0550be96d's pre-fix shape: token UNIQUE inline, no standalone index,
// and the entity field list declaring the same table.
func TestInlineUniqueBesideEntityNeverRuns(t *testing.T) {
	hs := dcFixture(t, "UNIQUE", "", "")
	if len(hs) != 1 || hs[0].Line != 20 {
		t.Fatalf("want one finding on the CREATE TABLE (line 20), got %v", hs)
	}
	if !strings.Contains(hs[0].Message, "ticket UNIQUE") || !strings.Contains(hs[0].Message, "AutoMigrate") {
		t.Errorf("message should name the column and the AutoMigrate ordering: %s", hs[0].Message)
	}
	// REFERENCES inline has the same fate.
	if hs := dcFixture(t, "REFERENCES users(id)", "", ""); len(hs) != 1 || !strings.Contains(hs[0].Message, "ticket REFERENCES") {
		t.Fatalf("want the inline REFERENCES reported, got %v", hs)
	}
}

// dbe11ba19's pre-fix shape (0550be96d's post-fix): the inline UNIQUE,
// the standalone unique index AND the entity field's Unique flag. The
// inline spelling reports twice over (never runs beside the entity, and
// doubles the index when it does), the field once.
func TestUniqueDeclaredBesideStandaloneIndex(t *testing.T) {
	hs := dcFixture(t, "UNIQUE", dcUniqueIndex, ", Unique: true")
	if len(hs) != 3 {
		t.Fatalf("want 3 findings (inline never runs, inline doubles, field doubles), got %d: %v", len(hs), hs)
	}
	var inline, field int
	for _, h := range hs {
		switch {
		case h.Line == 20 && strings.Contains(h.Message, "two unique indexes"):
			inline++
		case h.Line == 34 && strings.Contains(h.Message, "marked Unique"):
			field++
		}
	}
	if inline != 1 || field != 1 {
		t.Fatalf("want the inline doubling on line 20 and the field on line 34, got %v", hs)
	}
}

// The fix spelling (the tree as it is): no UNIQUE inline, no Unique on
// the field, the standalone index the only one.
func TestStandaloneUniqueIndexIsQuiet(t *testing.T) {
	if hs := dcFixture(t, "", dcUniqueIndex, ""); len(hs) != 0 {
		t.Fatalf("one standalone unique index; want quiet, got %v", hs)
	}
}

// Silent postures and near-misses.
func TestDDLConstraintQuietPostures(t *testing.T) {
	// Entity field Unique beside an inline UNIQUE, no standalone index:
	// whichever creates the table, one index exists.
	if hs := dcFixture(t, "", "", ", Unique: true"); len(hs) != 0 {
		t.Fatalf("field Unique alone; want quiet, got %v", hs)
	}
	// A partial unique index is not the column's UNIQUE.
	partial := `fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s (ticket) WHERE owner_id <> ''", on("_ticket")),`
	if hs := dcFixture(t, "", partial, ", Unique: true"); len(hs) != 0 {
		t.Fatalf("partial unique index; want quiet, got %v", hs)
	}
	// A composite unique index is not either.
	composite := `fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s (ticket, owner_id)", on("_ticket_owner")),`
	if hs := dcFixture(t, "", composite, ", Unique: true"); len(hs) != 0 {
		t.Fatalf("composite unique index; want quiet, got %v", hs)
	}
	// A unique index on ANOTHER table's column of the same name.
	other := `fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_ticket ON audit (ticket)"),`
	if hs := dcFixture(t, "", other, ", Unique: true"); len(hs) != 0 {
		t.Fatalf("index on another table; want quiet, got %v", hs)
	}
	// A column spelled through a verb is invisible, and PRIMARY KEY is
	// not a constraint the rule reads.
	src := `package vault

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

type DB interface{ Exec(string, ...any) }

func ensure(db DB, emailCol string) {
	db.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS members (id TEXT PRIMARY KEY, %s TEXT UNIQUE NOT NULL, name TEXT, roles TEXT)", emailCol))
}

func MemberFields() []schema.Field {
	return []schema.Field{{Name: "name", Type: schema.String}, {Name: "roles", Type: schema.String}}
}
`
	if hs := hits1416(fixture(t, map[string]string{"battery/vault/members.go": src})); len(hs) != 0 {
		t.Fatalf("verb-spelled column; want quiet, got %v", hs)
	}
	// No entity field list matches: a store-only table keeps its inline
	// UNIQUE (it is the only creator). A field list naming a column the
	// table lacks is not this table's entity.
	src = `package vault

import (
	"github.com/DonaldMurillo/gofastr/core/schema"
)

type DB interface{ Exec(string, ...any) }

func ensure(db DB) {
	db.Exec("CREATE TABLE IF NOT EXISTS keys (id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, created_at BIGINT)")
}

func OtherFields() []schema.Field {
	return []schema.Field{{Name: "name", Type: schema.String}, {Name: "colour", Type: schema.String}}
}
`
	if hs := hits1416(fixture(t, map[string]string{"battery/vault/keys.go": src})); len(hs) != 0 {
		t.Fatalf("no matching entity; want quiet, got %v", hs)
	}
}

// A plain CREATE TABLE (no IF NOT EXISTS) runs or fails loudly: arm (a)
// is quiet on it, but arm (b) still reads its inline UNIQUE beside a
// standalone index.
func TestPlainCreateTable(t *testing.T) {
	src := `package vault

type DB interface{ Exec(string, ...any) }

func ensure(db DB) {
	db.Exec("CREATE TABLE seats (id TEXT PRIMARY KEY, code TEXT UNIQUE)")
	db.Exec("CREATE UNIQUE INDEX seats_code ON seats (code)")
}
`
	hs := hits1416(fixture(t, map[string]string{"framework/vault/seats.go": src}))
	if len(hs) != 1 || !strings.Contains(hs[0].Message, "two unique indexes") || hs[0].Line != 6 {
		t.Fatalf("want the doubled unique on line 6, got %v", hs)
	}
}
