package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// A decorated Gated handler keeps its gates visible to registration: the
// gates run once, in order, before argument validation and before the
// decoration, and a refused caller gets the unknown-tool answer for any
// arguments (#669).
func TestDecorateKeepsGatesFirst(t *testing.T) {
	var calls []string
	allow := func(name string) func(context.Context) error {
		return func(context.Context) error { calls = append(calls, name); return nil }
	}
	refuse := func(context.Context) error { calls = append(calls, "refuse"); return errors.New("private") }
	handler := func(context.Context, map[string]any) (any, error) { calls = append(calls, "handler"); return "ok", nil }
	decorate := func(inner ToolHandler) ToolHandler {
		return func(ctx context.Context, params map[string]any) (any, error) {
			calls = append(calls, "decoration")
			return inner(ctx, params)
		}
	}

	s := NewServer()
	if err := s.RegisterTool("allowed", "", gateOracleInputSchema, Decorate(Gated(allow("outer"), Gated(allow("inner"), handler)), decorate)); err != nil {
		t.Fatal(err)
	}
	refused := Decorate(Gated(refuse, handler), decorate)
	if err := s.RegisterTool("refused", "", gateOracleInputSchema, refused); err != nil {
		t.Fatal(err)
	}

	calls = nil
	got, err := s.CallTool(context.Background(), "allowed", map[string]any{"secret": "valid"})
	if err != nil || got != "ok" || !reflect.DeepEqual(calls, []string{"outer", "inner", "decoration", "handler"}) {
		t.Fatalf("allowed call = %v (%v), callbacks %v; want gates once, then decoration and handler", got, err, calls)
	}
	calls = nil
	_, err = s.CallTool(context.Background(), "allowed", map[string]any{})
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != ErrInvalidParams || !reflect.DeepEqual(calls, []string{"outer", "inner"}) {
		t.Fatalf("allowed invalid call = %v, callbacks %v; want gates before validation", err, calls)
	}

	for _, args := range []map[string]any{{"secret": "valid"}, {}, {"secret": 42}, {"secret": "valid", "extra": true}} {
		calls = nil
		_, err := s.CallTool(context.Background(), "refused", args)
		if !errors.As(err, &rpc) || rpc.Code != ErrMethodNotFound || rpc.Message != `tool "refused" not found` ||
			!reflect.DeepEqual(calls, []string{"refuse"}) {
			t.Fatalf("refused call %v = %v, callbacks %v; want the unknown-tool answer after one gate run", args, err, calls)
		}
	}

	// Called directly, outside any server, the result is still gated.
	calls = nil
	if _, err := refused(context.Background(), nil); !errors.Is(err, errGatedHidden) || !reflect.DeepEqual(calls, []string{"refuse"}) {
		t.Fatalf("direct call = %v, callbacks %v; want the gate to refuse before the decoration", err, calls)
	}
}
