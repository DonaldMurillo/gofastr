package mcp

import (
	"context"
	"errors"
	"testing"
)

// Gated wraps a ToolHandler with a caller-facing precondition: the
// building block for auth-gating custom MCP tools. The gate sees the
// tool call's context (which carries the inbound HTTP request and
// whatever identity the router middleware resolved onto it).
func TestGatedBlocksWhenGateRefuses(t *testing.T) {
	ran := false
	h := Gated(
		func(ctx context.Context) error { return errors.New("auth: sign in required") },
		func(ctx context.Context, params map[string]any) (any, error) {
			ran = true
			return "secret", nil
		},
	)
	// A gate refusal surfaces as a caller-facing *RPCError carrying the
	// gate's message, so tools/call passes it through verbatim instead of
	// genericising it to "internal tool error" (the treatment a bare
	// handler error gets). Same contract as checkToolGate for WithToolGate.
	_, err := h(context.Background(), nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Message != "auth: sign in required" {
		t.Fatalf("want gate error surfaced as *RPCError, got %v", err)
	}
	if ran {
		t.Fatal("handler ran despite gate refusal")
	}
}

func TestGatedRunsHandlerWhenGateAllows(t *testing.T) {
	h := Gated(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context, params map[string]any) (any, error) { return 42, nil },
	)
	out, err := h(context.Background(), nil)
	if err != nil || out != 42 {
		t.Fatalf("want 42/nil, got %v/%v", out, err)
	}
}

func TestGatedNilGatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil gate must panic at construction, not silently allow")
		}
	}()
	Gated(nil, func(ctx context.Context, params map[string]any) (any, error) { return nil, nil })
}
