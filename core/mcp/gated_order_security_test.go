package mcp

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
)

// Allowed gates run once, before validation, and handler identity survives
// nested wrappers, reuse across servers, and garbage collection.
func TestGatedValidationOrderAndIdentity(t *testing.T) {
	var calls []string
	gate := func(name string) func(context.Context) error {
		return func(context.Context) error { calls = append(calls, name); return nil }
	}
	handler := func(context.Context, map[string]any) (any, error) { calls = append(calls, "handler"); return "ok", nil }
	wrapped := Gated(gate("outer"), Gated(gate("inner"), handler))
	denied := Gated(func(context.Context) error { return errors.New("private") }, handler)
	runtime.GC()
	for _, s := range []*Server{NewServer(), NewServer()} {
		if err := s.RegisterTool("allowed", "", gateOracleInputSchema, wrapped, WithToolGate(gate("item"))); err != nil {
			t.Fatal(err)
		}
		if err := s.RegisterTool("denied", "", gateOracleInputSchema, denied); err != nil {
			t.Fatal(err)
		}
		calls = nil
		_, err := s.CallTool(context.Background(), "allowed", map[string]any{})
		var rpc *RPCError
		if !errors.As(err, &rpc) || rpc.Code != ErrInvalidParams || !reflect.DeepEqual(calls, []string{"item", "outer", "inner"}) {
			t.Fatalf("invalid call = %v, callbacks %v; want gate chain once and no handler", err, calls)
		}
		calls = nil
		got, err := s.CallTool(context.Background(), "allowed", map[string]any{"secret": "valid"})
		if err != nil || got != "ok" || !reflect.DeepEqual(calls, []string{"item", "outer", "inner", "handler"}) {
			t.Fatalf("valid call = %v (%v), callbacks %v; want gate chain and handler once", got, err, calls)
		}
		calls = nil
		_, err = s.CallTool(context.Background(), "denied", map[string]any{})
		if !errors.As(err, &rpc) || rpc.Code != ErrMethodNotFound || len(calls) != 0 {
			t.Fatalf("refused wrapper = %v, callbacks %v; confused distinct wrappers", err, calls)
		}
		// Tool.Handler remains a gated callable for hosts reading ListTools.
		for _, tool := range s.ListTools() {
			if tool.Name == "denied" {
				_, err := tool.Handler(context.Background(), nil)
				if !errors.Is(err, errGatedHidden) {
					t.Fatalf("listed handler lost its gate: %v", err)
				}
			}
		}
	}
}

// Handler replacement through a ToolOption is still registered with the
// replacement's gate. The originally passed function is not authoritative.
func TestGatedToolOptionReplacement(t *testing.T) {
	s := NewServer()
	replacement := Gated(func(context.Context) error { return errors.New("hidden") }, func(context.Context, map[string]any) (any, error) { t.Fatal("refused handler ran"); return nil, nil })
	err := s.RegisterTool("replaced", "", gateOracleInputSchema, func(context.Context, map[string]any) (any, error) { t.Fatal("original handler ran"); return nil, nil }, func(tool *Tool) { tool.Handler = replacement })
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CallTool(context.Background(), "replaced", nil)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != ErrMethodNotFound {
		t.Fatalf("replacement gate = %v, want unknown tool", err)
	}
}
