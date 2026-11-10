package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// SECURITY: a masked column's stored value never leaves through the
// relation the column is the key of.
//
// A field with a Mask is NoQuery, so no filter, sort or search reads the
// stored value (schema.Field.Mask), and a field a read hook masks is
// marked NoQuery by hand. When that field is a belongs_to's foreign key,
// its stored value is the related record's key: an include answers it
// beside the masked column, and a nested filter across the relation is a
// filter on the column itself. entity.HidesRelation therefore hides the
// relation, as it hides one behind a Hidden key, and every surface below
// answers it as a relation that does not exist.
//
// The bug this holds: HidesRelation read only Hidden, so
// GET /payments/p1?include=account answered
// {"account":{"id":"ACCT-STORED-0001"},"account_id":"****0001"} and
// ?account.id_like=STORED-0001 selected the row, while ?account_id= was
// the not_filterable 400.

const maskedFKStored = "ACCT-STORED"

// lastFour is the fixture's mask: the stored key's last four characters.
func lastFour(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	return "****" + s[max(len(s)-4, 0):]
}

type maskedFKFixture struct {
	r        *router.Router
	accounts *CrudHandler
	payments *CrudHandler
}

func (f *maskedFKFixture) serve(method, target, body string) (int, string) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	f.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (f *maskedFKFixture) get(target string) (int, string) {
	return f.serve(http.MethodGet, target, "")
}

// maskedFKWorld is public accounts, ledgers and payments. A payment holds
// two references to an account: account_id, masked, under the belongs_to
// "account" (cascade_write on); and payer_ref, a masked relation field,
// from which Define derives a belongs_to of the same name. Its ledger_id
// is a plain relation field. An account has_many payments by account_id.
//
// mode "mask" declares the two masks on the fields. mode "hooks" masks
// them in AfterGet/AfterList hooks and marks the fields NoQuery by hand,
// the way an app spec does it.
func maskedFKWorld(t *testing.T, mode string) *maskedFKFixture {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE accounts (id TEXT PRIMARY KEY, label TEXT)`,
		`CREATE TABLE ledgers (id TEXT PRIMARY KEY, label TEXT)`,
		`CREATE TABLE payments (id TEXT PRIMARY KEY, account_id TEXT, payer_ref TEXT, ledger_id TEXT, memo TEXT)`,
	)
	public := &entity.ExposureConfig{Public: true}
	accountID := schema.Field{Name: "account_id", Type: schema.String}
	payerRef := schema.Field{Name: "payer_ref", Type: schema.Relation, To: "accounts"}
	hooks := hook.NewHookRegistry()
	switch mode {
	case "mask":
		mask := func(_ context.Context, v any) any { return lastFour(v) }
		accountID.Mask, payerRef.Mask = mask, mask
	case "hooks":
		accountID.NoQuery, payerRef.NoQuery = true, true
		maskRow := func(row map[string]any) {
			for _, k := range []string{"account_id", "payer_ref"} {
				if v, ok := row[k]; ok && v != nil {
					row[k] = lastFour(v)
				}
			}
		}
		hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
			maskRow(data.(*hook.GetPayload).Result)
			return nil
		})
		hooks.RegisterHook(hook.AfterList, func(_ context.Context, data any) error {
			for _, row := range data.(*hook.ListPayload).Results {
				maskRow(row)
			}
			return nil
		})
	default:
		t.Fatalf("unknown mode %q", mode)
	}
	accounts := entity.Define("accounts", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "label", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("payments", "payments", "account_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	ledgers := entity.Define("ledgers", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "id", Type: schema.String}, {Name: "label", Type: schema.String}},
		Exposure: public,
	}.WithTimestamps(false))
	payments := entity.Define("payments", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			accountID,
			payerRef,
			{Name: "ledger_id", Type: schema.Relation, To: "ledgers"},
			{Name: "memo", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("account", "accounts", "account_id").WithCascadeWrite(true)},
		Exposure:  public,
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"accounts": accounts, "ledgers": ledgers, "payments": payments}}
	f := &maskedFKFixture{r: router.New()}
	mount := func(e *entity.Entity) *CrudHandler {
		e.SetDB(db)
		ch := NewCrudHandler(e, db).WithJSONCase(CaseSnake)
		ch.Registry = reg
		ch.ChildHooks = func(name string) *hook.HookRegistry {
			if name == "payments" {
				return hooks
			}
			return nil
		}
		RegisterCrudRoutes(f.r, ch, "/"+e.GetName())
		return ch
	}
	f.accounts = mount(accounts)
	mount(ledgers)
	f.payments = mount(payments)
	f.payments.Hooks = hooks
	seedRows(t, db, "accounts", []map[string]any{
		{"id": "ACCT-STORED-0001", "label": "first"},
		{"id": "ACCT-STORED-0002", "label": "second"},
	})
	seedRows(t, db, "ledgers", []map[string]any{{"id": "LEDGER-1", "label": "ledger"}})
	seedRows(t, db, "payments", []map[string]any{
		{"id": "p1", "account_id": "ACCT-STORED-0001", "payer_ref": "ACCT-STORED-0001", "ledger_id": "LEDGER-1", "memo": "one"},
		{"id": "p2", "account_id": "ACCT-STORED-0002", "payer_ref": "ACCT-STORED-0002", "ledger_id": "LEDGER-1", "memo": "two"},
	})
	return f
}

func eachMaskedFKMode(t *testing.T, fn func(t *testing.T, f *maskedFKFixture)) {
	for _, mode := range []string{"mask", "hooks"} {
		t.Run(mode, func(t *testing.T) { fn(t, maskedFKWorld(t, mode)) })
	}
}

// Every include and nested-filter surface answers the relation behind a
// masked key the way it answers a name that is no relation: no stored
// value, no row selected by one, and no sign the relation exists.
func TestMaskedForeignKeyRelationIsNotIncludable(t *testing.T) {
	eachMaskedFKMode(t, func(t *testing.T, f *maskedFKFixture) {
		// Control: the mask works and the column's own filter is refused,
		// so what follows is about the relation only.
		if code, body := f.get("/payments/p1"); code != http.StatusOK || !strings.Contains(body, `"account_id":"****0001"`) ||
			!strings.Contains(body, `"payer_ref":"****0001"`) || strings.Contains(body, maskedFKStored) {
			t.Fatalf("control: GET /payments/p1 = %d %s; want the masked keys only", code, body)
		}
		if code, body := f.get("/payments?account_id=ACCT-STORED-0001"); code != http.StatusBadRequest || !strings.Contains(body, "not_filterable") {
			t.Fatalf("control: ?account_id= = %d %s; want the not_filterable 400", code, body)
		}

		// The answer a name that is no relation gets, with the name taken
		// out.
		unknown := func(target, name string) string {
			code, body := f.get(strings.ReplaceAll(target, "REL", name))
			return http.StatusText(code) + " " + strings.ReplaceAll(body, name, "REL")
		}
		for _, target := range []string{
			"/payments/p1?include=REL",
			"/payments?include=REL",
			"/payments?cursor=&include=REL",
			"/payments?stream=true&include=REL",
			"/payments?include=REL(id=ACCT-STORED-0001)",
			"/payments?include=REL(label=first)",
			"/accounts?include=payments.REL",
			"/payments?REL.id=ACCT-STORED-0001",
			"/payments?REL.id_like=STORED-0001",
			"/payments?REL.id[like]=STORED-0001",
			"/payments?REL.label=first",
			"/accounts?payments.REL.label=first",
		} {
			want := unknown(target, "nosuchrel")
			if !strings.HasPrefix(want, "Bad Request ") {
				t.Fatalf("premise: GET %s with an unknown relation answered %s", target, want)
			}
			for _, rel := range []string{"account", "payer_ref"} {
				if got := unknown(target, rel); got != want {
					t.Errorf("SECURITY: GET %s answered %s\nwant the unknown-relation answer %s",
						strings.ReplaceAll(target, "REL", rel), got, want)
				}
			}
		}

		// In process too, with and without the read-hook opt-in. Without
		// it a read answers stored values anyway, so nothing is kept from
		// that caller; the lookup is one rule on every surface, as it is
		// for a relation behind a Hidden key.
		for name, ctx := range map[string]context.Context{"stored": context.Background(), "read hooks": crudReadHooksCtx()} {
			for _, rel := range []string{"account", "payer_ref"} {
				if row, err := f.payments.GetOne(ctx, "p1", []string{rel}); err == nil {
					t.Errorf("%s GetOne(include %s) = %v, want the unknown-include error", name, rel, row)
				}
				if rows, err := f.payments.ListAll(ctx, ListOptions{Includes: []string{rel}}); err == nil {
					t.Errorf("%s ListAll(include %s) = %v, want the unknown-include error", name, rel, rows)
				}
				nested := []NestedFilter{{Relation: rel, Field: "id", Op: filter.OpEq, Value: "ACCT-STORED-0001"}}
				if rows, err := f.payments.ListAll(ctx, ListOptions{NestedFilters: nested}); err == nil {
					t.Errorf("%s ListAll(nested filter on %s) = %v, want the unknown-relation error", name, rel, rows)
				}
				if n, err := f.payments.CountAll(ctx, ListOptions{NestedFilters: nested}); err == nil {
					t.Errorf("%s CountAll(nested filter on %s) = %d, want the unknown-relation error", name, rel, n)
				}
				type payment struct {
					ID string `json:"id"`
				}
				if _, err := NewTypedQuery[payment](f.payments).Include(rel).Find(ctx); err == nil {
					t.Errorf("%s TypedQuery.Include(%s).Find succeeded, want the unknown-include error", name, rel)
				}
			}
		}

		// A near miss names only a relation the caller may include.
		for _, guess := range []string{"accounts", "account_id", "payer"} {
			if _, body := f.get("/payments?include=" + guess); strings.Contains(body, `"account"`) ||
				strings.Contains(body, `account\"`) || strings.Contains(body, "payer_ref") {
				t.Errorf("?include=%s hinted at the relation behind a masked key: %s", guess, body)
			}
		}

		// The plain relation still loads, and carries no masked key out.
		code, body := f.get("/payments?include=ledger_id")
		if code != http.StatusOK || !strings.Contains(body, `"label":"ledger"`) || strings.Contains(body, maskedFKStored) {
			t.Errorf("GET /payments?include=ledger_id = %d %s; want the ledger and no stored key", code, body)
		}
		// So does the has_many from the other side, whose key lives on
		// the target: an account lists its payments (includes.md, the
		// Hidden-key default), each with its key masked.
		code, body = f.get("/accounts/ACCT-STORED-0002?include=payments")
		if code != http.StatusOK || !strings.Contains(body, `"memo":"two"`) || !strings.Contains(body, `"account_id":"****0002"`) {
			t.Errorf("GET /accounts/ACCT-STORED-0002?include=payments = %d %s; want its payment with the key masked", code, body)
		}
	})
}

// A nested write through the relation would answer the attached account,
// key and all, beside the masked column. It is refused before any row is
// written, like a nested write through a relation behind a Hidden key;
// server code still writes through it.
func TestCascadeRefusesMaskedForeignKeyRelation(t *testing.T) {
	eachMaskedFKMode(t, func(t *testing.T, f *maskedFKFixture) {
		count := func() (accounts, payments int, label string) {
			t.Helper()
			if err := f.payments.DB.QueryRowContext(context.Background(),
				`SELECT (SELECT COUNT(*) FROM accounts), (SELECT COUNT(*) FROM payments), (SELECT label FROM accounts WHERE id = 'ACCT-STORED-0001')`,
			).Scan(&accounts, &payments, &label); err != nil {
				t.Fatal(err)
			}
			return accounts, payments, label
		}
		for _, c := range []struct{ method, target, body string }{
			{http.MethodPost, "/payments", `{"id":"p9","memo":"new","account":{"id":"ACCT-NEW-0009","label":"made"}}`},
			{http.MethodPatch, "/payments/p1", `{"memo":"moved","account":{"id":"ACCT-STORED-0001","label":"renamed"}}`},
			{http.MethodPut, "/payments/p1", `{"memo":"moved","account":{"label":"renamed"}}`},
		} {
			code, body := f.serve(c.method, c.target, c.body)
			if code != http.StatusBadRequest || !strings.Contains(body, "cascade_relation_hidden") || strings.Contains(body, maskedFKStored) {
				t.Errorf("SECURITY: %s %s through the relation = %d %s, want 400 cascade_relation_hidden", c.method, c.target, code, body)
			}
			if a, p, label := count(); a != 2 || p != 2 || label != "first" {
				t.Errorf("refused %s %s left accounts=%d payments=%d label=%q, want 2, 2, first", c.method, c.target, a, p, label)
			}
		}
		if _, body := f.get("/payments/p1"); !strings.Contains(body, `"memo":"one"`) {
			t.Errorf("a refused nested write changed the payment's own fields: %s", body)
		}

		// The key itself is still writable, and reads back masked.
		code, body := f.serve(http.MethodPatch, "/payments/p1", `{"account_id":"ACCT-STORED-0002"}`)
		if code != http.StatusOK || !strings.Contains(body, `"account_id":"****0002"`) || strings.Contains(body, maskedFKStored) {
			t.Errorf("PATCH /payments/p1 account_id = %d %s; want 200 with the masked key", code, body)
		}

		// Server code writes through the relation, as it writes a Hidden
		// field.
		if _, err := f.payments.CreateOne(WithServerWrites(context.Background()), map[string]any{
			"id": "p8", "memo": "server", "account": map[string]any{"id": "ACCT-NEW-0008", "label": "server"},
		}); err != nil {
			t.Fatalf("server write through the relation: %v", err)
		}
		if a, p, _ := count(); a != 3 || p != 3 {
			t.Errorf("server write left accounts=%d payments=%d, want 3 and 3", a, p)
		}
	})
}

// llm.md documents the includes an entity answers; the relation behind a
// masked key is not one. The masked field itself is still documented.
func TestLLMMDNamesNoMaskedForeignKeyRelation(t *testing.T) {
	eachMaskedFKMode(t, func(t *testing.T, f *maskedFKFixture) {
		doc := EntityLLMMD(f.payments.Entity)
		if !strings.Contains(doc, "`accountId`") || !strings.Contains(doc, "`payerRef`") {
			t.Fatalf("llm.md lost the masked fields:\n%s", doc)
		}
		_, includes, ok := strings.Cut(doc, "## Includes")
		if !ok {
			t.Fatalf("llm.md has no Includes section:\n%s", doc)
		}
		includes, _, _ = strings.Cut(includes, "\n## ")
		if !strings.Contains(includes, "| `ledger_id` |") {
			t.Fatalf("llm.md lost the plain relation:\n%s", includes)
		}
		for _, leak := range []string{"account", "payer"} {
			if strings.Contains(includes, leak) {
				t.Errorf("llm.md names the relation behind a masked key (%q):\n%s", leak, includes)
			}
		}
	})
}
