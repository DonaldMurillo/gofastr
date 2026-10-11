package crud

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// setupInvoiceWorld builds an owner-scoped invoices table: two owners'
// rows, statuses, amounts, a NoQuery note column and a Hidden column, so
// the Where/Fields surface has every refusal shape to hand.
func setupInvoiceWorld(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE invoices (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		status TEXT,
		amount INTEGER,
		note TEXT,
		internal_score TEXT,
		deleted_at TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	cfg := entity.EntityConfig{
		Name:  "invoices",
		Table: "invoices",
		Scope: &entity.ScopeConfig{OwnerField: "user_id", SoftDelete: true},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "status", Type: schema.String},
			{Name: "amount", Type: schema.Int},
			{Name: "note", Type: schema.String, NoQuery: true},
			{Name: "internal_score", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false)
	ent := entity.Define("invoices", cfg)
	ent.SetDB(db)
	installOwnerExtractor(t)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	return ch, db
}

func seedInvoices(t *testing.T, db *sql.DB, rows ...map[string]any) {
	t.Helper()
	seedRows(t, db, "invoices", rows)
}

func invoicePred(t *testing.T, text string) *filter.Predicate {
	t.Helper()
	p, err := dsl.ParsePredicate(text, []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "amount", Type: schema.Int},
		{Name: "user_id", Type: schema.String},
		{Name: "note", Type: schema.String, NoQuery: true},
		{Name: "internal_score", Type: schema.String, Hidden: true},
	})
	if err != nil {
		t.Fatalf("ParsePredicate(%q): %v", text, err)
	}
	return p
}

// The load-bearing scope test: an OR inside Where matching both owners'
// rows must still return only the caller's own rows. The scopes are
// separate ANDed clauses, so a user OR-group can never widen past them.
func TestListAllWhereORCannotWidenOwnerScope(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open", "amount": 100},
		map[string]any{"id": "b1", "user_id": "bob", "status": "open", "amount": 200},
	)

	p := invoicePred(t, `status = "open" or amount >= 0`) // matches both rows
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{Where: p})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != "a1" {
		t.Fatalf("SECURITY: Where OR widened past owner scope, alice saw %v", idsOf(rows))
	}

	n, err := ch.CountAll(ctxWithUser("alice"), ListOptions{Where: p})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CountAll = %d, want 1: the count must match the rows under the same scope", n)
	}
}

func TestListAllWhereFiltersRows(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open", "amount": 500},
		map[string]any{"id": "a2", "user_id": "alice", "status": "paid", "amount": 50},
	)
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{
		Where: invoicePred(t, `status = "open" and amount >= 100`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["id"] != "a1" {
		t.Fatalf("rows = %v", idsOf(rows))
	}
}

// contains must match the user's text literally: a 50% in the filter is a
// literal 50%, not a LIKE wildcard, or every trailing-number row leaks in
// (and a % in stored data becomes matchable).
func TestListAllWhereContainsMatchesLiteral(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "lit", "user_id": "alice", "status": "rate 50%"},
		map[string]any{"id": "wild", "user_id": "alice", "status": "rate 501"},
		map[string]any{"id": "mid", "user_id": "alice", "status": "a 50% b"},
	)
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{
		Where: invoicePred(t, `status contains "50%"`),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOf(rows)
	if len(got) != 2 || got[0] == "wild" || got[1] == "wild" {
		t.Fatalf("contains \"50%%\" matched %v, want only literal 50%% rows", got)
	}
	n, _ := ch.CountAll(ctxWithUser("alice"), ListOptions{Where: invoicePred(t, `status contains "50%"`)})
	if n != 2 {
		t.Fatalf("count = %d, want 2", n)
	}
}

// A hand-built Where is refused when it names anything the URL surface
// would refuse: the check runs inside ListAll, not only in the parsers.
func TestListAllWhereValidatedInPlace(t *testing.T) {
	ch, _ := setupInvoiceWorld(t)
	ctx := ctxWithUser("alice")
	for name, p := range map[string]*filter.Predicate{
		"unknown":     {Field: "nope", Op: filter.OpEq, Value: "x"},
		"hidden":      {Field: "internal_score", Op: filter.OpEq, Value: "x"},
		"noquery":     {Field: "note", Op: filter.OpEq, Value: "x"},
		"metachars":   {Field: "id; DROP TABLE invoices; --", Op: filter.OpEq, Value: "1"},
		"bad op/type": {Field: "amount", Op: filter.OpLike, Value: "5"},
	} {
		if _, err := ch.ListAll(ctx, ListOptions{Where: p}); err == nil {
			t.Errorf("ListAll accepted a Where naming a %s field", name)
		}
		if _, err := ch.CountAll(ctx, ListOptions{Where: p}); err == nil {
			t.Errorf("CountAll accepted a Where naming a %s field", name)
		}
	}
}

func TestListAllWhereNilIsNoop(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db, map[string]any{"id": "a1", "user_id": "alice", "status": "open"})
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// Projection: only the asked columns plus id come back.
func TestListAllFieldsProjectsColumns(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db, map[string]any{"id": "a1", "user_id": "alice", "status": "open", "amount": 5, "note": "n"})
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{Fields: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	row := rows[0]
	if row["status"] != "open" {
		t.Fatalf("projected column missing: %v", row)
	}
	if row["id"] != "a1" {
		t.Fatalf("id must always be included: %v", row)
	}
	for _, absent := range []string{"amount", "note", "user_id"} {
		if _, ok := row[absent]; ok {
			t.Errorf("unasked column %q came back: %v", absent, row)
		}
	}
}

func TestListAllFieldsRefuses(t *testing.T) {
	ch, _ := setupInvoiceWorld(t)
	ctx := ctxWithUser("alice")
	for name, fields := range map[string][]string{
		"unknown": {"nope"},
		"hidden":  {"internal_score"},
	} {
		if _, err := ch.ListAll(ctx, ListOptions{Fields: fields}); err == nil {
			t.Errorf("ListAll accepted projecting a %s field", name)
		}
		if _, err := ch.CountAll(ctx, ListOptions{Fields: fields}); err == nil {
			t.Errorf("CountAll accepted projecting a %s field", name)
		}
	}
}

// Masking keeps applying to a projected read: an AfterList hook that
// rewrites a column sees the projected rows too.
func TestListAllFieldsStillRunsAfterList(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db, map[string]any{"id": "a1", "user_id": "alice", "status": "open"})
	ch.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		if lp, ok := data.(*hook.ListPayload); ok {
			for _, r := range lp.Results {
				r["status"] = "MASKED"
			}
		}
		return nil
	})
	rows, err := ch.ListAll(WithReadHooks(ctxWithUser("alice")), ListOptions{Fields: []string{"status"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["status"] != "MASKED" {
		t.Fatalf("AfterList mask did not reach the projected read: %v", rows)
	}
}

// Where composes with flat Filters and the owner scope, each carrying its
// own bound args: the placeholders must line up so every clause binds its
// own value.
func TestListAllWhereWithFilters(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open", "amount": 500},
		map[string]any{"id": "a2", "user_id": "alice", "status": "open", "amount": 5},
		map[string]any{"id": "a3", "user_id": "alice", "status": "paid", "amount": 500},
		map[string]any{"id": "b1", "user_id": "bob", "status": "open", "amount": 500},
	)
	opts := ListOptions{
		Filters: []filter.ParsedFilter{{Field: "status", Op: filter.OpEq, Value: "open"}},
		Where:   invoicePred(t, `amount >= 100 or amount = 1`),
	}
	rows, err := ch.ListAll(ctxWithUser("alice"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(rows); len(got) != 1 || got[0] != "a1" {
		t.Fatalf("rows = %v, want [a1]", got)
	}
	if n, err := ch.CountAll(ctxWithUser("alice"), opts); err != nil || n != 1 {
		t.Fatalf("CountAll = %d, %v; want 1", n, err)
	}
}

// NoQuery keeps a field out of filters and sorts, not out of the row: a
// list that shows a NoQuery column must be able to fetch it.
func TestListAllFieldsReadsNoQueryColumn(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db, map[string]any{"id": "a1", "user_id": "alice", "status": "open", "note": "n"})
	rows, err := ch.ListAll(ctxWithUser("alice"), ListOptions{Fields: []string{"note"}})
	if err != nil {
		t.Fatalf("NoQuery projection refused: %v", err)
	}
	if len(rows) != 1 || rows[0]["note"] != "n" {
		t.Fatalf("NoQuery column not read: %v", rows)
	}
}

func idsOf(rows []map[string]any) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i], _ = r["id"].(string)
	}
	return out
}

// Hand-built ListOptions.Filters pass the same operator/type rule
// ?field_<op>= passes: a `like` on an Int column is refused by ListAll
// and CountAll alike, while the operators that suit the column still
// work. This is what makes the change note's "every filter surface"
// true for the in-process list too.
func TestListAllRefusesLikeOnIntFilter(t *testing.T) {
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open", "amount": 500},
	)
	bad := ListOptions{Filters: []filter.ParsedFilter{
		{Field: "amount", Op: filter.OpLike, Value: "5"},
	}}
	if _, err := ch.ListAll(ctxWithUser("alice"), bad); err == nil {
		t.Fatal("ListAll accepted a hand-built like filter on an Int column")
	}
	if _, err := ch.CountAll(ctxWithUser("alice"), bad); err == nil {
		t.Fatal("CountAll accepted a hand-built like filter on an Int column")
	}
	good := ListOptions{Filters: []filter.ParsedFilter{
		{Field: "amount", Op: filter.OpGte, Value: "100"},
	}}
	rows, err := ch.ListAll(ctxWithUser("alice"), good)
	if err != nil {
		t.Fatalf("ListAll refused a suited hand-built filter: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %v, want the one seeded row", rows)
	}
}

// A hand-built filter may name a field by its WireName, the spelling
// ?field= accepts; the SQL names the column, so the list resolves it.
func TestListAllFilterResolvesWireName(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY, amount INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO items (id, amount) VALUES ('a1', 500), ('a2', 50)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("items", entity.EntityConfig{
		Name:   "items",
		Table:  "items",
		Fields: []schema.Field{{Name: "amount", WireName: "total", Type: schema.Int}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	ch.Hooks = hook.NewHookRegistry()

	opts := ListOptions{Filters: []filter.ParsedFilter{{Field: "total", Op: filter.OpGte, Value: "100"}}}
	rows, err := ch.ListAll(context.Background(), opts)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if got := idsOf(rows); len(got) != 1 || got[0] != "a1" {
		t.Fatalf("rows = %v, want [a1]", got)
	}
	if n, err := ch.CountAll(context.Background(), opts); err != nil || n != 1 {
		t.Fatalf("CountAll = %d, %v; want 1", n, err)
	}
	if opts.Filters[0].Field != "total" {
		t.Error("ListAll rewrote the caller's filter slice")
	}
}
