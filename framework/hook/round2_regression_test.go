package hook

import (
	"context"
	"testing"
)

func TestZeroValueHookRegistryRegisters(t *testing.T) {
	var registry HookRegistry
	var called bool
	registry.RegisterHook(BeforeGet, func(_ context.Context, _ any) error { called = true; return nil })
	if err := registry.ExecuteHooks(t.Context(), BeforeGet, nil); err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestWhereClausesOwnArguments(t *testing.T) {
	args := []any{"original"}
	var list ListPayload
	var get GetPayload
	list.AddWhere("status = $1", args...)
	get.AddWhere("status = $1", args...)
	args[0] = "changed"
	if list.Where[0].Args[0] != "original" || get.Where[0].Args[0] != "original" {
		t.Fatal("editing the source arguments changed a registered clause")
	}
}
