package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestRegisterHookFiredOnRegister(t *testing.T) {
	s := NewServer()
	var got []string
	s.SetRegisterHook(func(name string) {
		got = append(got, name)
	})

	if err := s.RegisterTool("a", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}
	if err := s.RegisterTool("b", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}

	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("expected [a b], got %v", got)
	}
}

func TestRegisterHookNilIsNoop(t *testing.T) {
	s := NewServer()
	s.SetRegisterHook(nil)
	if err := s.RegisterTool("x", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("RegisterTool: %v", err)
	}
}

func TestCallGateBlocksDisabledTool(t *testing.T) {
	s := NewServer()
	s.SetCallGate(func(name string) error {
		if name == "blocked" {
			return errGateTest
		}
		return nil
	})
	s.RegisterTool("blocked", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		t.Fatal("handler should not run")
		return nil, nil
	})
	s.RegisterTool("open", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		return "ok", nil
	})

	// Blocked tool → error without invoking handler.
	_, err := s.CallTool(context.Background(), "blocked", nil)
	if err == nil {
		t.Fatal("expected error from gated tool")
	}

	// Open tool → runs normally.
	res, err := s.CallTool(context.Background(), "open", nil)
	if err != nil {
		t.Fatalf("open tool: %v", err)
	}
	if res != "ok" {
		t.Fatalf("expected 'ok', got %v", res)
	}
}

func TestCallGateNilIsNoop(t *testing.T) {
	s := NewServer()
	s.SetCallGate(nil)
	s.RegisterTool("x", "desc", map[string]any{"type": "object"}, func(ctx context.Context, _ map[string]any) (any, error) {
		return "ran", nil
	})
	res, err := s.CallTool(context.Background(), "x", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "ran" {
		t.Fatalf("expected 'ran', got %v", res)
	}
}

// TestListToolsExcludesGated verifies that tools whose call gate
// refuses them are absent from ListTools (L4).
func TestListToolsExcludesGated(t *testing.T) {
	s := NewServer()
	s.SetCallGate(func(name string) error {
		if name == "blocked" {
			return errGateTest
		}
		return nil
	})
	s.RegisterTool("blocked", "desc", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) {
		return nil, nil
	})
	s.RegisterTool("open", "desc", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) {
		return nil, nil
	})

	tools := s.ListTools()
	for _, tool := range tools {
		if tool.Name == "blocked" {
			t.Fatal("gated tool 'blocked' should not appear in ListTools")
		}
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "open" {
			found = true
		}
	}
	if !found {
		t.Fatal("non-gated tool 'open' should appear in ListTools")
	}
}

// TestCallGateGenericRefusalMessage verifies the RPC error for a gated
// tool does not leak module identity (L4).
func TestCallGateGenericRefusalMessage(t *testing.T) {
	s := NewServer()
	s.SetCallGate(func(name string) error {
		if name == "blocked" {
			return errGateTest
		}
		return nil
	})
	s.RegisterTool("blocked", "desc", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) {
		return nil, nil
	})

	_, err := s.CallTool(context.Background(), "blocked", nil)
	if err == nil {
		t.Fatal("expected error for gated tool")
	}
	// The message must be generic — no module name, no "disabled" state.
	msg := err.Error()
	if msg != "tool unavailable" && !strings.Contains(msg, "tool unavailable") {
		t.Fatalf("expected generic 'tool unavailable', got %q", msg)
	}
	if strings.Contains(msg, "module") || strings.Contains(msg, "disabled") {
		t.Fatalf("refusal message leaks module identity: %q", msg)
	}
}

// TestCallGateConcurrentSafe exercises SetCallGate concurrent with
// CallTool to verify no data race (M4).
func TestCallGateConcurrentSafe(t *testing.T) {
	s := NewServer()
	s.SetCallGate(func(name string) error { return nil })
	s.RegisterTool("hot", "desc", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) {
		return "ok", nil
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			s.SetCallGate(func(name string) error { return nil })
		}
	}()

	for i := 0; i < 200; i++ {
		s.CallTool(context.Background(), "hot", nil)
	}
	<-done
}

// errGateTest is a sentinel error used by TestCallGateBlocksDisabledTool.
var errGateTest = mcpTestError("module disabled")

type mcpTestError string

func (e mcpTestError) Error() string { return string(e) }
