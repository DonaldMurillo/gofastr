package acp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/acp"
)

func TestSweepSessionSetupErrorTextStaysInside(t *testing.T) {
	const secret = "dial tcp 10.0.0.7:5432: password=hunter2"
	agent := &fakeLoadingAgent{
		fakeAgent: fakeAgent{newFn: func(context.Context, string) (acp.Session, error) {
			return nil, errors.New(secret)
		}},
		loadFn: func(context.Context, string, string, *acp.Client) (acp.Session, error) {
			return nil, errors.New(secret)
		},
	}
	d := startDialog(t, agent, nil)
	d.initialize()
	d.request(2, "session/new", map[string]any{"cwd": "/tmp/p", "mcpServers": []any{}})
	d.request(3, "session/load", map[string]any{"sessionId": "s1", "cwd": "/tmp/p", "mcpServers": []any{}})
	for _, id := range []int{2, 3} {
		e, _ := d.untilResponseID(id)["error"].(map[string]any)
		if e == nil {
			t.Fatalf("request %d: a failing agent call was not refused", id)
		}
		if msg, _ := e["message"].(string); containsSecret(msg, secret) {
			t.Fatalf("request %d: embedder error text reached the client: %q", id, msg)
		}
	}
}

func TestSweepEnvelopeRefusesUnknownKey(t *testing.T) {
	d := startDialog(t, &fakeAgent{}, nil)
	d.send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}, "extra": true})
	f := d.frame()
	e, _ := f["error"].(map[string]any)
	if e == nil || e["code"] != float64(acp.ErrParseError) {
		t.Fatalf("an envelope with an unknown key was not refused -32700: %v", f)
	}
}
