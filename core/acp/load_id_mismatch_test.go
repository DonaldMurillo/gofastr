package acp_test

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

// session/load checks the requested id for a turn in progress and then
// recorded the loaded session under whatever id the loader returned. A
// loader answering a load of s2 with s1's session replaced s1 while its
// turn ran: the busy flag went with the old entry, and a second prompt
// on s1 ran beside the first. A returned id that differs from the
// requested one is refused.
func TestLoadRefusesDifferentSessionID(t *testing.T) {
	started := make(chan struct{}, 2)
	s1 := &fakeSession{id: "s1", promptFn: func(ctx context.Context, _ []acp.ContentBlock, _ *acp.Client) (string, error) {
		started <- struct{}{}
		<-ctx.Done()
		return acp.StopCancelled, nil
	}}
	agent := &fakeLoadingAgent{
		fakeAgent: fakeAgent{newFn: func(context.Context, string) (acp.Session, error) { return s1, nil }},
		loadFn: func(_ context.Context, id, _ string, _ *acp.Client) (acp.Session, error) {
			return &fakeSession{id: "s1", promptFn: s1.promptFn}, nil
		},
	}
	d := startDialog(t, agent, nil)
	sid := d.newSession("/tmp/p")
	d.request(3, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "go"}}})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt turn never started")
	}
	d.request(4, "session/load", map[string]any{"sessionId": "s2", "cwd": "/tmp/p", "mcpServers": []any{}})
	if f := d.untilResponseID(4); f["error"] == nil {
		t.Errorf("session/load of s2 answered with s1's session: %v, want an error", f)
	}
	d.request(5, "session/prompt", map[string]any{"sessionId": sid, "prompt": []any{map[string]any{"type": "text", "text": "again"}}})
	if e := respError(t, d.untilResponseID(5)); e["code"] != float64(acp.ErrInvalidRequest) {
		t.Fatalf("second prompt on busy s1: code = %v, want %d", e["code"], acp.ErrInvalidRequest)
	}
	d.send(map[string]any{"jsonrpc": "2.0", "method": "session/cancel", "params": map[string]any{"sessionId": sid}})
	d.untilResponseID(3)
}
