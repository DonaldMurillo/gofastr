package mcp

import (
	"context"
	"testing"
)

// HasTool must see every registered name, including tools the filtered
// listings hide, because it exists to answer "is this name taken",
// not "is this tool visible".
func TestHasToolSeesGatedTools(t *testing.T) {
	s := NewServer()
	handler := func(ctx context.Context, params map[string]any) (any, error) { return nil, nil }
	if err := s.RegisterTool("hidden_tool", "d", nil, handler); err != nil {
		t.Fatalf("register: %v", err)
	}
	s.SetCallGate(func(name string) error {
		return context.Canceled // every tool refused → hidden from listings
	})

	if len(s.ListTools()) != 0 {
		t.Fatal("precondition: gated tool should be hidden from ListTools")
	}
	if !s.HasTool("hidden_tool") {
		t.Fatal("HasTool must report gated-but-registered names as taken")
	}
	if s.HasTool("absent_tool") {
		t.Fatal("HasTool must not report unregistered names")
	}
}
