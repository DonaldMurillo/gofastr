package framework

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/access"
)

func TestModuleTogglesWriteActorAudit(t *testing.T) {
	db := newStoreDB(t)
	defer db.Close()
	app := NewApp(WithDB(db), WithMCPControl())
	app.WithAuditLog(AuditConfig{Table: "operator_audit"})
	app.RegisterModule(&modStub{name: "reports", manifest: ModuleManifest{}, init: noopInit})
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	ctx := access.WithRoles(handler.SetUser(context.Background(), &auditActorUser{id: "operator-27"}), []string{"admin"})
	for _, tool := range []string{"app_module_disable", "app_module_enable"} {
		if _, err := app.MCP.CallTool(ctx, tool, map[string]any{"name": "reports"}); err != nil {
			t.Fatal(err)
		}
	}
	// The direct manager API must share the trail with MCP.
	if err := app.Modules().Disable(ctx, "reports"); err != nil {
		t.Fatal(err)
	}
	if err := app.Modules().Enable(context.Background(), "unknown"); err == nil {
		t.Fatal("unknown module enabled")
	}
	rows, err := db.Query(`SELECT entity, op, record_id, actor_id FROM operator_audit ORDER BY created_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ops []string
	for rows.Next() {
		var entity, op, record, actor string
		if err := rows.Scan(&entity, &op, &record, &actor); err != nil {
			t.Fatal(err)
		}
		if entity != "module" || record != "reports" || actor != "operator-27" {
			t.Fatalf("wrong audit attribution: %q %q %q", entity, record, actor)
		}
		ops = append(ops, op)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(ops) != 3 || ops[0] != "module_disable" || ops[1] != "module_enable" || ops[2] != "module_disable" {
		t.Fatalf("module audit ops=%v", ops)
	}
}

func TestModuleAuditSurvivesCancellation(t *testing.T) {
	db := newStoreDB(t)
	defer db.Close()
	app := NewApp(WithDB(db))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	app.WithAuditLog(AuditConfig{Actor: func(context.Context) string { cancel(); return "custom-actor" }})
	app.RegisterModule(&modStub{name: "reports", manifest: ModuleManifest{}, init: noopInit})
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if err := app.Modules().Disable(ctx, "reports"); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := db.QueryRow(`SELECT actor_id FROM audit_log WHERE op='module_disable'`).Scan(&actor); err != nil || actor != "custom-actor" {
		t.Fatalf("audit after cancellation: actor=%q err=%v", actor, err)
	}
}
