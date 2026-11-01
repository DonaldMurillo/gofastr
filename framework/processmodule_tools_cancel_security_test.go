package framework

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// A module tool call whose caller gives up must tell the child, the way
// a proxied HTTP call does: module.cancel names the frame id so the
// child's handler context ends. The tool path used plain Call and never
// sent it, so the child kept working until its own deadline.
func TestToolCallCancelReachesChild(t *testing.T) {
	sup, _, _, child, cleanup := toolTestSlot(t, nil)
	defer cleanup()
	cancelled := make(chan struct{}, 1)
	if err := child.Handle(moduleproto.MethodToolCall, func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-ctx.Done():
			cancelled <- struct{}{}
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
			return moduleproto.ToolCallResult{Result: jsonRaw(`"late"`)}, nil
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := sup.dispatchToolCall(ctx, "demo", "search", map[string]any{"q": "x"}); err == nil {
		t.Fatal("tool call past its deadline returned no error")
	}
	select {
	case <-cancelled:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("module.cancel never reached the child after the tool call's deadline")
	}
}
