package entityui

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestBulkHonorsBeforeGet(t *testing.T) {
	for _, action := range []string{"set:priority:high", "move:send", "delete", "restore"} {
		t.Run(action, func(t *testing.T) {
			ents := invoiceEntities()
			invoice := ents["invoices"]
			invoice.Scope = &entity.ScopeConfig{SoftDelete: true}
			invoice.Fields = append(invoice.Fields, schema.Field{Name: "priority", Type: schema.Enum, Values: []string{"low", "high"}})
			ents["invoices"] = invoice
			rows := invoiceRows()
			rows["invoices"][0]["priority"] = "low"
			rows["invoices"] = append(rows["invoices"], map[string]any{"id": "inv-2", "number": "INV-2", "status": "draft", "priority": "low"})
			x := newTestUI(t, ents, rows, withAPI(map[string]string{"invoices": "/api/invoices"}))
			ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
			if err != nil {
				t.Fatal(err)
			}
			ch.Hooks = hook.NewHookRegistry()
			ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
				data.(*hook.GetPayload).AddWhere("id <> $1", "inv-1")
				return nil
			})
			ctx := bulkCtx("u1", nil)
			if _, err := ch.GetOne(crud.WithReadHooks(ctx), "inv-1", nil); !crud.IsNotFound(err) {
				t.Fatalf("hidden row get = %v, want not found", err)
			}
			scope := "selected"
			if action == "restore" {
				scope = "deleted"
				if _, err := x.db.Exec(`UPDATE invoices SET deleted_at = CURRENT_TIMESTAMP`); err != nil {
					t.Fatal(err)
				}
			}
			code, out := postBulk(t, x, ctx, map[string]any{"action": action, "scope": scope, "ids": []string{"inv-1", "inv-2"}})
			if code != http.StatusOK || out["done"] != float64(1) {
				t.Errorf("bulk = %d %v, want only the visible row done", code, out)
			}
			var status, priority string
			var deleted bool
			if err := x.db.QueryRow(`SELECT status, priority, deleted_at IS NOT NULL FROM invoices WHERE id = 'inv-1'`).Scan(&status, &priority, &deleted); err != nil {
				t.Fatal(err)
			}
			if status != "draft" || priority != "low" || deleted != (action == "restore") {
				t.Errorf("hidden row changed: status=%q priority=%q deleted=%v", status, priority, deleted)
			}
		})
	}
}

func TestBulkAppActionCrudHonorsBeforeGet(t *testing.T) {
	ran := false
	ext := Extensions{Entities: map[string]Extension{"invoices": {Actions: []Action{{
		Key: "check", Bulk: true,
		Run: func(ctx context.Context, action ActionContext) error {
			ran = true
			_, err := action.Crud.UpdateOne(ctx, "inv-1", map[string]any{"number": "changed"})
			if !crud.IsNotFound(err) {
				t.Errorf("callback write to hidden row = %v, want not found", err)
			}
			return nil
		},
	}}}}}
	rows := invoiceRows()
	rows["invoices"] = append(rows["invoices"], map[string]any{"id": "inv-2", "number": "INV-2", "status": "draft"})
	x := newTestUIExt(t, invoiceEntities(), rows, ext, withAPI(map[string]string{"invoices": "/api/invoices"}))
	ch, err := x.host.Crud(mustEntity(t, x, "invoices"))
	if err != nil {
		t.Fatal(err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
		data.(*hook.GetPayload).AddWhere("id <> $1", "inv-1")
		return nil
	})
	code, output := postBulk(t, x, bulkCtx("u1", nil), map[string]any{"action": "run:check", "scope": "selected", "ids": []string{"inv-2"}})
	if code != http.StatusOK || !ran {
		t.Fatalf("bulk callback = %d %v, ran=%v", code, output, ran)
	}
	var number string
	if err := x.db.QueryRow(`SELECT number FROM invoices WHERE id = 'inv-1'`).Scan(&number); err != nil {
		t.Fatal(err)
	}
	if number != "INV-1" {
		t.Errorf("hidden row number = %q, want INV-1", number)
	}
}
