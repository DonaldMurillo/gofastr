package crud

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func seedAggregateInvoices(t *testing.T) *CrudHandler {
	t.Helper()
	ch, db := setupInvoiceWorld(t)
	seedInvoices(t, db,
		map[string]any{"id": "a1", "user_id": "alice", "status": "open", "amount": 10},
		map[string]any{"id": "a2", "user_id": "alice", "status": "open", "amount": 20},
		map[string]any{"id": "a3", "user_id": "alice", "status": "closed", "amount": 40},
		map[string]any{"id": "a4", "user_id": "alice", "status": "closed", "amount": 80, "deleted_at": "2026-01-01"},
		map[string]any{"id": "b1", "user_id": "bob", "status": "open", "amount": 1000},
	)
	return ch
}

// SumAll totals every row a list read would return to the caller: the
// owner, soft-delete and BeforeList scopes and the caller's Where all
// narrow it, and nothing caps it.
func TestSumAllHonorsListScopes(t *testing.T) {
	ch := seedAggregateInvoices(t)
	ctx := WithReadHooks(ctxWithUser("alice"))
	if got, err := ch.SumAll(ctx, "amount", ListOptions{}); err != nil || got != "70" {
		t.Fatalf("SumAll = %q, %v; want 70 (alice's live rows)", got, err)
	}
	open := &filter.Predicate{Field: "status", Op: filter.OpEq, Value: "open"}
	if got, err := ch.SumAll(ctx, "amount", ListOptions{Where: open}); err != nil || got != "30" {
		t.Fatalf("SumAll where open = %q, %v; want 30", got, err)
	}
	scopeToOpen(ch)
	if got, err := ch.SumAll(ctx, "amount", ListOptions{}); err != nil || got != "30" {
		t.Fatalf("SECURITY: SumAll ignored BeforeList: %q, %v; want 30", got, err)
	}
	none := &filter.Predicate{Field: "status", Op: filter.OpEq, Value: "void"}
	if got, err := ch.SumAll(ctx, "amount", ListOptions{Where: none}); err != nil || got != "0" {
		t.Fatalf("SumAll over no rows = %q, %v; want 0", got, err)
	}
}

// GroupCountAll counts the caller's rows per value, ordered by value,
// under the same scopes, and caps the groups at limit.
func TestGroupCountAllHonorsListScopes(t *testing.T) {
	ch := seedAggregateInvoices(t)
	ctx := WithReadHooks(ctxWithUser("alice"))
	got, err := ch.GroupCountAll(ctx, "status", ListOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []GroupCount{{Value: "closed", Count: 1}, {Value: "open", Count: 2}}
	if !slices.Equal(got, want) {
		t.Fatalf("GroupCountAll = %v, want %v", got, want)
	}
	if got, _ := ch.GroupCountAll(ctx, "status", ListOptions{}, 1); len(got) != 1 {
		t.Fatalf("limit 1 returned %v", got)
	}
	scopeToOpen(ch)
	got, err = ch.GroupCountAll(ctx, "status", ListOptions{}, 0)
	if err != nil || !slices.Equal(got, []GroupCount{{Value: "open", Count: 2}}) {
		t.Fatalf("SECURITY: GroupCountAll ignored BeforeList: %v, %v", got, err)
	}
}

// On Postgres a Decimal column sums as NUMERIC: the total is exact where
// float64 addition is not, and groups keep their $N placeholders inside
// the wrapped read.
func TestAggregatesOnPostgres(t *testing.T) {
	cfg := entity.EntityConfig{
		Name: "ledger", Table: "ledger",
		Fields: []schema.Field{
			{Name: "kind", Type: schema.String},
			{Name: "amount", Type: schema.Decimal},
		},
	}.WithTimestamps(false)
	ch, db := pgCrudSetup(t, cfg, `CREATE TABLE ledger (id TEXT PRIMARY KEY, kind TEXT, amount DECIMAL(19,4))`)
	if _, err := db.Exec(`INSERT INTO ledger (id, kind, amount) SELECT 'r' || x, CASE WHEN x % 2 = 0 THEN 'a' ELSE 'b' END, 0.1 FROM generate_series(1, 30) AS x`); err != nil {
		t.Fatal(err)
	}
	ctx := WithReadHooks(context.Background())
	got, err := ch.SumAll(ctx, "amount", ListOptions{Where: &filter.Predicate{Field: "kind", Op: filter.OpEq, Value: "a"}})
	if err != nil || got != "1.5000" {
		t.Fatalf("SumAll = %q, %v; want the exact 1.5000", got, err)
	}
	groups, err := ch.GroupCountAll(ctx, "kind", ListOptions{Where: &filter.Predicate{Field: "amount", Op: filter.OpGt, Value: "0"}}, 0)
	if want := []GroupCount{{Value: "a", Count: 15}, {Value: "b", Count: 15}}; err != nil || !slices.Equal(groups, want) {
		t.Fatalf("GroupCountAll = %v, %v; want %v", groups, err, want)
	}
}

// A bool group carries the value a list read would: true or false, not
// SQLite's stored 1 or 0.
func TestGroupCountAllNormalizesBool(t *testing.T) {
	db := openBooleanReadDB(t)
	if _, err := db.Exec(`CREATE TABLE flags (id TEXT, active INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO flags (id, active) VALUES ('f1', 1), ('f2', 0), ('f3', 1)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("flags", entity.EntityConfig{
		Fields: []schema.Field{{Name: "active", Type: schema.Bool}},
	}.WithTimestamps(false))
	got, err := NewCrudHandler(ent, db).GroupCountAll(context.Background(), "active", ListOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []GroupCount{{Value: false, Count: 1}, {Value: true, Count: 2}}; !slices.Equal(got, want) {
		t.Fatalf("GroupCountAll = %#v, want %#v", got, want)
	}
}

// An aggregate refuses what it cannot honor: a field it may not read or
// cannot total, list paging, and AfterList hooks on a read that runs them.
func TestAggregateRefusals(t *testing.T) {
	ch := seedAggregateInvoices(t)
	ctx := WithReadHooks(ctxWithUser("alice"))
	for name, call := range map[string]func() error{
		"hidden":    func() error { _, err := ch.SumAll(ctx, "internal_score", ListOptions{}); return err },
		"unknown":   func() error { _, err := ch.GroupCountAll(ctx, "nope", ListOptions{}, 0); return err },
		"string":    func() error { _, err := ch.SumAll(ctx, "status", ListOptions{}); return err },
		"limit":     func() error { _, err := ch.SumAll(ctx, "amount", ListOptions{Limit: 2}); return err },
		"hidden gc": func() error { _, err := ch.GroupCountAll(ctx, "internal_score", ListOptions{}, 0); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: aggregate ran, want a refusal", name)
		}
	}
	ch.Hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
		for _, r := range data.(*hook.ListPayload).Results {
			r["amount"] = 0
		}
		return nil
	})
	if _, err := ch.SumAll(ctx, "amount", ListOptions{}); !errors.Is(err, ErrAggregateMasked) {
		t.Fatalf("SECURITY: SumAll under AfterList = %v, want ErrAggregateMasked", err)
	}
	if _, err := ch.GroupCountAll(ctx, "status", ListOptions{}, 0); !errors.Is(err, ErrAggregateMasked) {
		t.Fatalf("SECURITY: GroupCountAll under AfterList = %v, want ErrAggregateMasked", err)
	}
	if got, err := ch.SumAll(ctxWithUser("alice"), "amount", ListOptions{}); err != nil || got != "70" {
		t.Fatalf("SumAll without read hooks = %q, %v; want 70", got, err)
	}
}
