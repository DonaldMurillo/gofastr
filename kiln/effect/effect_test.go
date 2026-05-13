package effect_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/effect"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func newEffect(kind string, params map[string]any) world.Action {
	return world.Action{Kind: kind, Params: params}
}

func TestNoop(t *testing.T) {
	if err := effect.Run(context.Background(), newEffect(world.ActionNoop, nil), effect.Scope{}); err != nil {
		t.Fatalf("noop: %v", err)
	}
}

func TestValidatePassesAndFails(t *testing.T) {
	data := map[string]any{"title": "hello"}
	scope := effect.Scope{Entity: data}

	if err := effect.Run(context.Background(),
		newEffect(world.ActionValidate, map[string]any{"expression": "len(entity.title) > 0", "message": "title required"}),
		scope); err != nil {
		t.Errorf("expected pass, got %v", err)
	}

	data["title"] = ""
	if err := effect.Run(context.Background(),
		newEffect(world.ActionValidate, map[string]any{"expression": "len(entity.title) > 0", "message": "title required"}),
		scope); err == nil {
		t.Errorf("expected validation failure")
	} else if !strings.Contains(err.Error(), "title required") {
		t.Errorf("error should include message: %v", err)
	}
}

func TestSetFieldFromExpression(t *testing.T) {
	data := map[string]any{"title": "hello"}
	scope := effect.Scope{Entity: data}
	if err := effect.Run(context.Background(),
		newEffect(world.ActionSetField, map[string]any{"field": "slug", "value": "lower(entity.title)"}),
		scope); err != nil {
		t.Fatalf("set_field: %v", err)
	}
	if data["slug"] != "hello" {
		t.Errorf("slug = %v, want hello", data["slug"])
	}
}

func TestSetFieldLiteralWhenNotExpression(t *testing.T) {
	// Plain string that doesn't parse as an expression: treat as literal.
	// We force literal via a leading backtick — see Run/setField docs.
	data := map[string]any{}
	scope := effect.Scope{Entity: data}
	if err := effect.Run(context.Background(),
		newEffect(world.ActionSetField, map[string]any{"field": "status", "value_literal": "draft"}),
		scope); err != nil {
		t.Fatalf("set_field literal: %v", err)
	}
	if data["status"] != "draft" {
		t.Errorf("status = %v", data["status"])
	}
}

func TestAuditEmitsRecord(t *testing.T) {
	var captured []effect.AuditRecord
	scope := effect.Scope{
		Entity: map[string]any{"title": "hello"},
		Audit: func(rec effect.AuditRecord) {
			captured = append(captured, rec)
		},
	}
	if err := effect.Run(context.Background(),
		newEffect(world.ActionAudit, map[string]any{"channel": "create-log", "message": `"created: " + entity.title`}),
		scope); err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(captured) != 1 {
		t.Fatalf("expected 1 audit record, got %d", len(captured))
	}
	if captured[0].Channel != "create-log" {
		t.Errorf("channel = %q", captured[0].Channel)
	}
	if captured[0].Message != "created: hello" {
		t.Errorf("message = %q", captured[0].Message)
	}
}

func TestEmitEvent(t *testing.T) {
	var got []effect.EventRecord
	scope := effect.Scope{
		Entity: map[string]any{"id": int64(1)},
		Emit: func(rec effect.EventRecord) {
			got = append(got, rec)
		},
	}
	if err := effect.Run(context.Background(),
		newEffect(world.ActionEmitEvent, map[string]any{"topic": "posts.created", "data": "entity"}),
		scope); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(got) != 1 || got[0].Topic != "posts.created" {
		t.Fatalf("emit got %v", got)
	}
}

func TestUnknownKindErrors(t *testing.T) {
	err := effect.Run(context.Background(), newEffect("not_a_kind", nil), effect.Scope{})
	if err == nil {
		t.Fatal("unknown kind should error")
	}
}

func TestRunRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := effect.Run(ctx, newEffect(world.ActionNoop, nil), effect.Scope{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestConditionGate(t *testing.T) {
	data := map[string]any{"role": "admin"}
	scope := effect.Scope{Entity: data}

	hook := &world.Hook{
		ID:        "h1",
		Condition: `entity.role == "admin"`,
		Action:    newEffect(world.ActionValidate, map[string]any{"expression": "false", "message": "blocked"}),
	}
	// Condition true → action runs → fails validation → returns error.
	if err := effect.RunHook(context.Background(), hook, scope); err == nil {
		t.Error("expected validate error to surface")
	}

	data["role"] = "guest"
	// Condition false → action skipped → no error.
	if err := effect.RunHook(context.Background(), hook, scope); err != nil {
		t.Errorf("expected condition skip, got %v", err)
	}
}
