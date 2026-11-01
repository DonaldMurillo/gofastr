package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1415 exists because App.EraseUserData and App.ExportData reach
// only the entity graph and the tables registered in core/datexport,
// and the 2026-10-27 bug bash found fourteen stores that create a table
// holding user data and register neither (S9 battery/auth's OAuth token
// table beside erase.go's registrations for every other auth table, S10
// the outbox, S20 A2A, S22–S29 and S32). The fixtures reduce those
// shapes under different identifiers, pin the fixed spellings quiet
// (type-asserted fields, canonical literal defaults, helper parameters),
// and pin every silent posture.

const ecImports = `import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

var _ = fmt.Sprintf
var _ datexport.DataEraser
`

// hits1415 returns the GOFASTR1415 findings, sorted by file and line.
func hits1415(ds []contracts.Diagnostic) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, d := range ds {
		if d.RuleID == contracts.RuleEraseCover {
			out = append(out, d)
		}
	}
	return out
}

// S9, reduced: the package registers both planes for its session store
// through a type assertion, and its token store's table, keyed by
// user_id, through nothing.
func TestTokenTableBesideErasedSessions(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/vault/sessions.go": `package vault

` + ecImports + `
type SessionStore struct{ db DB; table string }

func (s *SessionStore) ensure() {
	s.db.Exec(fmt.Sprintf(` + "`" + `CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)` + "`" + `, s.table))
}
`,
		"battery/vault/tokens.go": `package vault

` + ecImports + `
type TokenStore struct{ db DB; table string }

func (s *TokenStore) ensure() {
	s.db.Exec(fmt.Sprintf(` + "`" + `CREATE TABLE IF NOT EXISTS %s (
		user_id TEXT NOT NULL,
		access_token TEXT NOT NULL
	)` + "`" + `, s.table))
}
`,
		"battery/vault/erase.go": `package vault

` + ecImports + `
type DB interface{ Exec(string, ...any) }

func wire(store any) {
	if ss, ok := store.(*SessionStore); ok {
		datexport.RegisterEraser(datexport.DataEraser{Name: "sessions", Table: ss.table, Column: "user_id"})
		datexport.Register(datexport.DataExporter{Name: "sessions", Table: ss.table})
	}
}
`,
	})
	hs := hits1415(ds)
	if len(hs) != 1 {
		t.Fatalf("want exactly the token table, got %d: %v", len(hs), hs)
	}
	if hs[0].File != "battery/vault/tokens.go" || hs[0].Line != 15 {
		t.Errorf("finding at %s, want battery/vault/tokens.go:15 (the CREATE TABLE line)", hs[0].Location())
	}
	if !strings.Contains(hs[0].Message, "DataEraser") || !strings.Contains(hs[0].Message, "DataExporter") {
		t.Errorf("message should name both missing planes: %s", hs[0].Message)
	}
}

// The fix posture S9's fix idea names: register the token table too,
// once by type assertion and once under the canonical literal that the
// constructor's default spells.
func TestRegisteredTokenTableIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/vault/tokens.go": `package vault

` + ecImports + `
type DB interface{ Exec(string, ...any) }
type Config struct{ Table string }
type TokenStore struct{ db DB; table string }

func NewTokenStore(db DB, c Config) *TokenStore {
	t := c.Table
	if t == "" {
		t = "oauth_grants"
	}
	s := &TokenStore{db: db, table: t}
	s.ensure()
	return s
}

func (s *TokenStore) ensure() {
	s.db.Exec(fmt.Sprintf(` + "`" + `CREATE TABLE IF NOT EXISTS %s (user_id TEXT NOT NULL, access_token TEXT NOT NULL)` + "`" + `, s.table))
}
`,
		"battery/vault/erase.go": `package vault

` + ecImports + `
func init() {
	datexport.RegisterEraser(datexport.DataEraser{Name: "oauth_grants", Table: "oauth_grants", Column: "user_id"})
}

func wire(store any) {
	if ts, ok := store.(*TokenStore); ok {
		datexport.Register(datexport.DataExporter{Name: "oauth_grants", Table: ts.table})
	}
}
`,
	})
	if hs := hits1415(ds); len(hs) != 0 {
		t.Fatalf("both planes are declared (literal default + asserted field), got %v", hs)
	}
}

// Erased but never exported: the S32 audit-log shape, and the
// magic-link token shape. The finding names only the missing plane.
func TestErasedButNotExportedNamesExportPlane(t *testing.T) {
	ds := fixture(t, map[string]string{
		"framework/ledger/ledger.go": `package ledger

` + ecImports + `
const ledgerTable = "activity_ledger"

func ensure(db interface{ Exec(string, ...any) }) {
	db.Exec(fmt.Sprintf(` + "`" + `CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, actor_id TEXT, diff TEXT)` + "`" + `, ledgerTable))
}

func init() {
	datexport.RegisterEraser(datexport.DataEraser{Name: "ledger", Table: ledgerTable, Column: "actor_id", Mode: datexport.EraseAnonymize})
}
`,
	})
	hs := hits1415(ds)
	if len(hs) != 1 {
		t.Fatalf("want one finding, got %v", hs)
	}
	if strings.Contains(hs[0].Message, "DataEraser") || !strings.Contains(hs[0].Message, "DataExporter") {
		t.Errorf("only the export plane is missing: %s", hs[0].Message)
	}
}

// S10, reduced: a payload table with no user column, named through a
// quoting method. Then the fix, declared through a helper whose table
// parameter is bound at its call site.
func TestPayloadTableThroughQuotingMethod(t *testing.T) {
	src := `package relay

` + ecImports + `
type Relay struct{ db interface{ Exec(string, ...any) }; table string }

func (r *Relay) qt() string { return quoteFolded(r.table) }

func quoteFolded(s string) string { return "\"" + s + "\"" }

func (r *Relay) ensure() {
	r.db.Exec(fmt.Sprintf(` + "`" + `CREATE TABLE IF NOT EXISTS %s (
		id      TEXT PRIMARY KEY,
		type    TEXT NOT NULL,
		payload TEXT
	)` + "`" + `, r.qt()))
}
`
	ds := fixture(t, map[string]string{"framework/relay/relay.go": src})
	if hs := hits1415(ds); len(hs) != 1 || hs[0].Line != 19 {
		t.Fatalf("want one finding on line 19 (the payload table), got %v", hs)
	}

	ds = fixture(t, map[string]string{
		"framework/relay/relay.go": src,
		"framework/relay/erase.go": `package relay

` + ecImports + `
func registerPlanes(table string) {
	datexport.RegisterEraser(datexport.DataEraser{Name: "relay", Table: table, Column: "user_id"})
	datexport.Register(datexport.DataExporter{Name: "relay", Table: table})
}

func (r *Relay) wire() { registerPlanes(r.table) }
`,
	})
	if hs := hits1415(ds); len(hs) != 0 {
		t.Fatalf("the helper binds r.table at its call site; want quiet, got %v", hs)
	}
}

// A registration for the base table does not cover a suffixed sibling:
// `s.table + "_attempts"` is its own table (the S30 rate-limit store's
// two-table shape, with a user column so the rule can see it).
func TestSuffixedSiblingIsItsOwnTable(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/limit/limit.go": `package limit

` + ecImports + `
type Store struct{ db interface{ Exec(string, ...any) }; table string }

func (s *Store) ensure() {
	s.db.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (user_id TEXT PRIMARY KEY, until BIGINT)", s.table))
	s.db.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (user_id TEXT NOT NULL, at BIGINT)", s.table+"_attempts"))
}

func (s *Store) wire() {
	datexport.RegisterEraser(datexport.DataEraser{Name: "limit", Table: s.table, Column: "user_id"})
	datexport.Register(datexport.DataExporter{Name: "limit", Table: s.table})
}
`,
	})
	hs := hits1415(ds)
	if len(hs) != 1 || hs[0].Line != 16 {
		t.Fatalf("want only the _attempts table (line 16), got %v", hs)
	}
	if !strings.Contains(hs[0].Message, "_attempts") {
		t.Errorf("message should name the suffixed table: %s", hs[0].Message)
	}
}

// S23, reduced: an FTS5 virtual table storing document text and fields;
// and a multi-statement literal reports each table on its own line.
func TestVirtualAndMultiStatementTables(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/find/find.go": `package find

` + ecImports + `
type Index struct{ db interface{ Exec(string, ...any) }; quoted string }

func (x *Index) ensure() {
	x.db.Exec(fmt.Sprintf(` + "`" + `CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(id UNINDEXED, fields UNINDEXED, text, tokenize='porter')` + "`" + `, x.quoted))
}

const schema = ` + "`" + `
CREATE TABLE IF NOT EXISTS notes (
    id    INTEGER PRIMARY KEY,
    owner TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS replies (
    id   INTEGER PRIMARY KEY,
    body TEXT NOT NULL
);
` + "`" + `
`,
	})
	hs := hits1415(ds)
	var lines []int
	for _, h := range hs {
		lines = append(lines, h.Line)
	}
	if len(hs) != 3 || lines[0] != 15 || lines[1] != 19 || lines[2] != 24 {
		t.Fatalf("want lines 15 (fts5), 19 (notes) and 24 (replies), got %v", lines)
	}
}

// Silent postures, each pinned.
func TestEraseCoverSilentPostures(t *testing.T) {
	ds := fixture(t, map[string]string{
		// Coordination state: no user column, no payload. claim_owner is
		// a worker id and only an exact column name counts.
		"framework/lease/lease.go": `package lease

` + ecImports + `
func ensure(db interface{ Exec(string, ...any) }) {
	db.Exec("CREATE TABLE IF NOT EXISTS cron_ticks (lease_key BIGINT PRIMARY KEY, last_tick TIMESTAMP)")
	db.Exec("CREATE TABLE IF NOT EXISTS occurrences (id TEXT PRIMARY KEY, claim_owner TEXT NOT NULL, owner_count INTEGER)")
}
`,
		// Columns spelled through verbs: the entity migrator's and the
		// configurable user table's shape. The rule reads only names.
		"framework/gen/gen.go": `package gen

` + ecImports + `
func ddl(table, idCol, emailCol string) string {
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY, %s TEXT UNIQUE NOT NULL)", table, idCol, emailCol)
}
`,
		// A quoted example (verb slot with no Sprintf argument) and
		// prose that says "create table" are not statements.
		"framework/docs/docs.go": `package docs

const example = "db.Exec(fmt.Sprintf(` + "`" + `CREATE TABLE IF NOT EXISTS %s (user_id TEXT)` + "`" + `, s.table))"

const prose = "A CREATE TABLE in core/ that names a column (user_id, payload) is checked."
`,
		// Outside core/, framework/ and battery/: an app's own table.
		"internal/store/store.go": `package store

func ensure(db interface{ Exec(string, ...any) }) {
	db.Exec("CREATE TABLE IF NOT EXISTS notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)")
}
`,
		// The documented exception: a reason on the line above.
		"core/cachebox/cache.go": `package cachebox

func ensure(db interface{ Exec(string, ...any) }) {
	//gofastr:allow(GOFASTR1415) rows expire within sixty seconds and hold only a hash
	db.Exec("CREATE TABLE IF NOT EXISTS replay (key TEXT PRIMARY KEY, body BLOB, expires_at BIGINT)")
}
`,
	})
	if hs := hits1415(ds); len(hs) != 0 {
		t.Fatalf("every site here is a silent posture, got %v", hs)
	}
}

// Near-miss: coverage declared from a different package than the one
// creating the table does not count. The registration must live where
// the table name is known.
func TestCoverageFromAnotherPackageDoesNotCount(t *testing.T) {
	ds := fixture(t, map[string]string{
		"core/tasks/store.go": `package tasks

func ensure(db interface{ Exec(string, ...any) }) {
	db.Exec("CREATE TABLE IF NOT EXISTS agent_tasks (id TEXT PRIMARY KEY, owner TEXT NOT NULL, task_json TEXT NOT NULL)")
}
`,
		"framework/mount.go": `package framework

` + ecImports + `
func init() {
	datexport.RegisterEraser(datexport.DataEraser{Name: "agent_tasks", Table: "agent_tasks", Column: "owner"})
	datexport.Register(datexport.DataExporter{Name: "agent_tasks", Table: "agent_tasks"})
}
`,
	})
	hs := hits1415(ds)
	if len(hs) != 1 || hs[0].File != "core/tasks/store.go" {
		t.Fatalf("want the core/tasks table reported, got %v", hs)
	}
}

// Near-miss: a registration naming a different literal table leaves
// this one uncovered, and a concatenated DDL names its table through
// the non-literal operand (the cron-ticks const shape).
func TestWrongLiteralDoesNotCover(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/notes/notes.go": `package notes

` + ecImports + `
const notesTable = "user_notes"

const ddl = ` + "`CREATE TABLE IF NOT EXISTS `" + ` + notesTable + ` + "` (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)`" + `

func init() {
	datexport.RegisterEraser(datexport.DataEraser{Name: "notes", Table: "notes", Column: "user_id"})
	datexport.Register(datexport.DataExporter{Name: "notes", Table: notesTable})
}
`,
	})
	hs := hits1415(ds)
	if len(hs) != 1 || !strings.Contains(hs[0].Message, "DataEraser") || strings.Contains(hs[0].Message, "no datexport.DataExporter") {
		t.Fatalf("want one finding naming only the eraser plane, got %v", hs)
	}
}

// A registration whose Table is set by field assignment after the
// literal (or on a zero value) covers the table the same way.
func TestFieldAssignedTableCovers(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/notes/notes.go": `package notes

` + ecImports + `
const notesTable = "notes"

func ensure(db interface{ Exec(string, ...any) }) {
	db.Exec("CREATE TABLE IF NOT EXISTS notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)")
}

func init() {
	var e datexport.DataEraser
	e.Name = "notes"
	e.Table = notesTable
	datexport.RegisterEraser(e)
	x := &datexport.DataExporter{Name: "notes"}
	x.Table = notesTable
	datexport.Register(*x)
}
`,
	})
	if hs := hits1415(ds); len(hs) != 0 {
		t.Fatalf("both planes are declared through field assignment, got %v", hs)
	}
}

// uid, member_id, data and content name a user or carry record data
// like the columns the rule already reads.
func TestEraseCoverWiderColumns(t *testing.T) {
	ds := fixture(t, map[string]string{
		"core/wide/wide.go": `package wide

func ensure(db interface{ Exec(string, ...any) }) {
	db.Exec("CREATE TABLE IF NOT EXISTS a (id TEXT PRIMARY KEY, uid TEXT)")
	db.Exec("CREATE TABLE IF NOT EXISTS b (id TEXT PRIMARY KEY, member_id TEXT)")
	db.Exec("CREATE TABLE IF NOT EXISTS c (id TEXT PRIMARY KEY, data BLOB)")
	db.Exec("CREATE TABLE IF NOT EXISTS d (id TEXT PRIMARY KEY, content TEXT)")
}
`,
	})
	if hs := hits1415(ds); len(hs) != 4 {
		t.Fatalf("want 4 findings (uid, member_id, data, content), got %d: %v", len(hs), hs)
	}
}

// A user column added by ALTER TABLE ADD COLUMN makes the table hold
// user data as surely as one in the CREATE; a non-user column added the
// same way does not, a covered table stays quiet, and an ALTER on a
// table whose CREATE already reports does not report twice.
func TestAlterAddColumnIsSeen(t *testing.T) {
	ds := fixture(t, map[string]string{
		"framework/widgets/widgets.go": `package widgets

` + ecImports + `
func migrate(db interface{ Exec(string, ...any) }) {
	db.Exec("CREATE TABLE IF NOT EXISTS widgets (id TEXT PRIMARY KEY)")
	db.Exec("ALTER TABLE widgets ADD COLUMN IF NOT EXISTS owner_id TEXT")
	db.Exec("ALTER TABLE widgets ADD COLUMN color TEXT")
	db.Exec("ALTER TABLE gadgets ADD COLUMN payload TEXT, ADD COLUMN size INT")
	db.Exec("CREATE TABLE IF NOT EXISTS gizmos (id TEXT PRIMARY KEY, user_id TEXT)")
	db.Exec("ALTER TABLE gizmos ADD COLUMN email TEXT")
}

func init() {
	datexport.RegisterEraser(datexport.DataEraser{Name: "gadgets", Table: "gadgets", Column: "id"})
	datexport.Register(datexport.DataExporter{Name: "gadgets", Table: "gadgets"})
}
`,
	})
	hs := hits1415(ds)
	if len(hs) != 2 || hs[0].Line != 14 || !strings.Contains(hs[0].Message, "owner_id") || hs[1].Line != 17 {
		t.Fatalf("want 2 findings: the owner_id ALTER (line 14) and the gizmos CREATE (line 17), not its ALTER; got %v", hs)
	}
}
