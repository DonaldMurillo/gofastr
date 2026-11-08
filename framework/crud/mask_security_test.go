package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A field's declared Mask rewrites what a read returns and leaves the
// stored value alone. The read paths each run it are pinned by the "mask"
// mode of TestReadHookParityCrud; these pin the rest of the contract.

type maskFixture struct {
	ch *CrudHandler
	r  *router.Router
	db *sql.DB
}

func maskCrud(t *testing.T) *maskFixture {
	t.Helper()
	db := setupDB(t, `CREATE TABLE cards (id TEXT PRIMARY KEY, number TEXT, pin INTEGER)`)
	ent := entity.Define("cards", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "number", Type: schema.String, Mask: func(_ context.Context, v any) any {
				if v == nil {
					t.Error("Mask was handed a NULL")
				}
				s, _ := v.(string)
				return "****" + s[max(len(s)-4, 0):]
			}},
			{Name: "pin", Type: schema.Int, Mask: func(context.Context, any) any { return 0 }},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	r := router.New()
	RegisterCrudRoutes(r, ch, "/cards")
	seedRows(t, db, "cards", []map[string]any{
		{"id": "c1", "number": "4111222233334444", "pin": 1234},
		{"id": "c2", "number": nil, "pin": nil},
	})
	return &maskFixture{ch: ch, r: r, db: db}
}

func (f *maskFixture) get(target string) (int, string) {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	f.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestMaskMarksFieldNoQuery(t *testing.T) {
	f := maskCrud(t)
	for _, fld := range f.ch.Entity.GetFields() {
		if fld.Mask != nil && !fld.NoQuery {
			t.Errorf("field %s has a Mask but is queryable", fld.Name)
		}
	}
	for _, q := range []string{"?number=4111222233334444", "?number_like=4111", "?sort=number", "?pin_gt=1000"} {
		if code, body := f.get("/cards" + q); code != http.StatusBadRequest || strings.Contains(body, "c1") {
			t.Errorf("GET /cards%s = %d %s, want 400: the stored value must not decide which rows match", q, code, body)
		}
	}
}

func TestMaskKeepsStoredValue(t *testing.T) {
	f := maskCrud(t)
	_, body := f.get("/cards/c1")
	if !strings.Contains(body, `"number":"****4444"`) || !strings.Contains(body, `"pin":0`) || strings.Contains(body, "4111") {
		t.Errorf("GET /cards/c1 = %s, want the masked number and pin", body)
	}
	_, body = f.get("/cards")
	if !strings.Contains(body, `"number":null`) {
		t.Errorf("GET /cards = %s, want the NULL number left null", body)
	}
	var stored string
	if err := f.db.QueryRow(`SELECT number FROM cards WHERE id = 'c1'`).Scan(&stored); err != nil || stored != "4111222233334444" {
		t.Errorf("stored number = %q (%v), want it unchanged by the reads", stored, err)
	}
	// In-process reads without the read-hook opt-in return stored values,
	// as they do for hooks: read-modify-write needs them.
	row, err := f.ch.GetOne(context.Background(), "c1", nil)
	if err != nil || row["number"] != "4111222233334444" {
		t.Errorf("GetOne without WithReadHooks = %v (%v), want the stored number", row, err)
	}
	row, err = f.ch.GetOne(WithReadHooks(context.Background()), "c1", nil)
	if err != nil || row["number"] != "****4444" {
		t.Errorf("GetOne under WithReadHooks = %v (%v), want the masked number", row, err)
	}
}

func TestMaskRunsBeforeAfterHooks(t *testing.T) {
	f := maskCrud(t)
	var seen any
	f.ch.Hooks = hook.NewHookRegistry()
	f.ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		seen = data.(*hook.GetPayload).Result["number"]
		return nil
	})
	f.get("/cards/c1")
	if seen != "****4444" {
		t.Errorf("AfterGet saw number %v, want the masked value", seen)
	}
}

func TestMaskRefusesAggregates(t *testing.T) {
	f := maskCrud(t)
	ctx := WithReadHooks(context.Background())
	if _, err := f.ch.SumAll(ctx, "pin", ListOptions{}); !errors.Is(err, ErrAggregateMasked) {
		t.Errorf("SumAll over a masked field = %v, want ErrAggregateMasked", err)
	}
	if _, err := f.ch.GroupCountAll(ctx, "number", ListOptions{}, 0); !errors.Is(err, ErrAggregateMasked) {
		t.Errorf("GroupCountAll over a masked field = %v, want ErrAggregateMasked", err)
	}
	if s, err := f.ch.SumAll(context.Background(), "pin", ListOptions{}); err != nil || s != "1234" {
		t.Errorf("SumAll without WithReadHooks = %q (%v), want the stored total", s, err)
	}
}

// An include of a relation named after its key column puts the related
// row where the column's value was; that row is its own entity's to mask.
// A JSON field's object is the field's own value and is masked.
func TestMaskSkipsOnlyIncludedRow(t *testing.T) {
	masked := func(context.Context, any) any { return "masked" }
	ent := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "author_id", Type: schema.Relation, To: "users", Mask: masked},
			{Name: "meta", Type: schema.JSON, Mask: masked},
		},
	}.WithTimestamps(false))
	related := map[string]any{"id": "x"}
	rows := []map[string]any{{"author_id": related, "meta": map[string]any{"secret": "s"}}}
	maskRows(context.Background(), ent, func(s string) string { return s }, rows)
	if got, ok := rows[0]["author_id"].(map[string]any); !ok || got["id"] != "x" {
		t.Errorf("an included row under a masked key became %v", rows[0]["author_id"])
	}
	if rows[0]["meta"] != "masked" {
		t.Errorf("a masked JSON object read back as %v, want the mask's value", rows[0]["meta"])
	}
}

// A child a nested write creates comes back in the parent's response
// under its own entity's Mask, with no hooks registered anywhere.
func TestMaskCoversNestedWriteChild(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE wallets (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE cards (id TEXT PRIMARY KEY, wallet_id TEXT, number TEXT)`,
	)
	cards := entity.Define("cards", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "wallet_id", Type: schema.String},
			{Name: "number", Type: schema.String, Mask: func(context.Context, any) any { return "****" }},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	wallets := entity.Define("wallets", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("cards", "cards", "wallet_id").WithCascadeWrite(true)},
		Exposure:  &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	cards.SetDB(db)
	wallets.SetDB(db)
	ch := NewCrudHandler(wallets, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"wallets": wallets, "cards": cards}}
	req := httptest.NewRequest(http.MethodPost, "/wallets", strings.NewReader(`{"id":"w1","name":"main","cards":[{"id":"k1","number":"5555666677778888"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated || strings.Contains(rec.Body.String(), "5555") || !strings.Contains(rec.Body.String(), `"number":"****"`) {
		t.Errorf("POST /wallets = %d %s, want the nested card masked", rec.Code, rec.Body.String())
	}
}

// A Mask that panics reads as null and the read goes on: masks run where
// no per-request recover catches a panic.
func TestMaskPanicReadsAsNull(t *testing.T) {
	f := maskCrud(t)
	for i, fld := range f.ch.Entity.Config.Fields {
		if fld.Name == "number" {
			f.ch.Entity.Config.Fields[i].Mask = func(context.Context, any) any { panic("bad card") }
		}
	}
	code, body := f.get("/cards/c1")
	if code != http.StatusOK || !strings.Contains(body, `"number":null`) || strings.Contains(body, "4111") {
		t.Errorf("GET with a panicking Mask = %d %s, want 200 and the number null", code, body)
	}
}
