package mcp

import (
	"context"
	"errors"
	"testing"
)

// CallToolFor is the in-process call for a remote caller on another
// surface: the server-wide gate applies to it as to tools/call, with the
// same refusal, and an allowing gate still runs the tool.
func TestCallToolForRunsServerGate(t *testing.T) {
	s := NewServer()
	ran := 0
	if err := s.RegisterTool("t", "d", nil, func(context.Context, map[string]any) (any, error) {
		ran++
		return "ok", nil
	}); err != nil {
		t.Fatal(err)
	}
	s.SetGate(func(context.Context) error { return errors.New("closed") })
	_, err := s.CallToolFor(context.Background(), "t", nil)
	rpcErr, ok := err.(*RPCError)
	if !ok || rpcErr.Code != ErrInvalidParams || rpcErr.Message != "closed" || ran != 0 {
		t.Fatalf("SECURITY: [authz] CallToolFor under a refusing gate: err %v ran %d, want invalid params \"closed\"", err, ran)
	}
	s.SetGate(func(context.Context) error { return nil })
	if out, err := s.CallToolFor(context.Background(), "t", nil); err != nil || out != "ok" || ran != 1 {
		t.Fatalf("an allowing gate: out %v err %v ran %d", out, err, ran)
	}
}
