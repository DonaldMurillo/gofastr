package integration_test

import (
	"context"
	"io"
	"strings"
	"testing"

	acpcore "github.com/DonaldMurillo/gofastr/core/acp"
	kilnacp "github.com/DonaldMurillo/gofastr/kiln/acp"
	"github.com/DonaldMurillo/gofastr/kiln/agent"
	"github.com/DonaldMurillo/gofastr/kiln/internal/kilntest"
)

// acpStdioClient drives a kiln ACP server over stdio pipes: the test
// plays the editor, the server plays the agent.
type acpStdioClient struct {
	*kilntest.PipeDialog
}

func startACPStdio(t *testing.T, h *harness, prov agent.Provider) *acpStdioClient {
	t.Helper()
	srv := kilnacp.NewServer(h.tools, kilnacp.WithProvider(prov))
	srvIn, clientW := io.Pipe()
	clientR, srvOut := io.Pipe()
	c := &acpStdioClient{kilntest.StartPipeDialog(t, clientW, clientR)}
	go func() { _ = srv.Serve(context.Background(), srvIn, srvOut) }()
	return c
}

// untilResponse collects notifications until the response with id
// (a frame that has an id but no method).
func (c *acpStdioClient) untilResponse(id int) (map[string]any, []map[string]any) {
	var notifs []map[string]any
	for {
		f := c.Frame()
		if got, ok := f["id"].(float64); ok && f["method"] == nil && int(got) == id {
			return f, notifs
		}
		notifs = append(notifs, f)
	}
}

// TestACPPromptTurnOverStdio drives one full ACP session over stdio:
// initialize → session/new → session/prompt with a scripted provider
// that calls add_entity and then replies, asserting the streamed
// tool-call round trip and the resulting world state.
func TestACPPromptTurnOverStdio(t *testing.T) {
	h := newHarness(t)
	prov := &kilntest.ScriptedProvider{Turns: []agent.Turn{
		{
			ToolCalls: []agent.ToolCall{{
				CallID: "call_1",
				Name:   "add_entity",
				Args: map[string]any{
					"entity": map[string]any{
						"name":   "posts",
						"fields": []any{map[string]any{"name": "title", "type": "string", "required": true}},
					},
				},
			}},
			StopReason: "tool_use",
		},
		{Text: "posts entity added", StopReason: "end_turn"},
	}}
	c := startACPStdio(t, h, prov)

	// initialize: kiln's agent must declare what it does NOT do.
	c.Request(1, "initialize", map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": true,
		},
		"clientInfo": map[string]any{"name": "kiln-e2e", "version": "0"},
	})
	initResp, _ := c.untilResponse(1)
	if initResp["error"] != nil {
		t.Fatalf("initialize: %v", initResp["error"])
	}
	caps := initResp["result"].(map[string]any)["agentCapabilities"].(map[string]any)
	pc := caps["promptCapabilities"].(map[string]any)
	for _, k := range []string{"image", "audio", "embeddedContext"} {
		if v, present := pc[k]; !present || v != false {
			t.Errorf("promptCapabilities.%s = %v (present=%v), want explicit false", k, v, present)
		}
	}
	if caps["loadSession"] != true {
		t.Errorf("loadSession = %v, kiln supports session/load", caps["loadSession"])
	}

	// session/new
	c.Request(2, "session/new", map[string]any{"cwd": t.TempDir(), "mcpServers": []any{}})
	newResp, _ := c.untilResponse(2)
	if newResp["error"] != nil {
		t.Fatalf("session/new: %v", newResp["error"])
	}
	sid := newResp["result"].(map[string]any)["sessionId"].(string)

	// session/prompt: the scripted provider calls add_entity, then
	// answers with text; the turn must stream every tool frame before
	// the end_turn response.
	c.Request(3, "session/prompt", map[string]any{
		"sessionId": sid,
		"prompt":    []any{map[string]any{"type": "text", "text": "add a posts entity"}},
	})
	promptResp, notifs := c.untilResponse(3)
	if promptResp["error"] != nil {
		t.Fatalf("session/prompt: %v", promptResp["error"])
	}
	if got := promptResp["result"].(map[string]any)["stopReason"]; got != acpcore.StopEndTurn {
		t.Errorf("stopReason = %v, want end_turn", got)
	}

	var seq []string
	for _, n := range notifs {
		if n["method"] != "session/update" {
			t.Fatalf("unexpected non-update frame during the turn: %v", n)
		}
		params := n["params"].(map[string]any)
		if params["sessionId"] != sid {
			t.Errorf("update sessionId = %v, want %q", params["sessionId"], sid)
		}
		u := params["update"].(map[string]any)
		seq = append(seq, u["sessionUpdate"].(string)+"/"+statusOf(u))
	}
	want := []string{
		"tool_call/pending",
		"tool_call_update/in_progress",
		"tool_call_update/completed",
		"agent_message_chunk/",
	}
	if strings.Join(seq, "|") != strings.Join(want, "|") {
		t.Fatalf("update sequence:\n got %v\nwant %v", seq, want)
	}

	// The tool round trip really hit the world.
	if _, ok := h.live.Session().World.Entities["posts"]; !ok {
		t.Error("posts entity missing after the ACP turn")
	}
	// And the conversation was journaled for the panel.
	if prov.Seen[len(prov.Seen)-1].Messages[0].Text != "add a posts entity" {
		t.Errorf("provider saw %q as the opening user message", prov.Seen[len(prov.Seen)-1].Messages[0].Text)
	}
}

func statusOf(u map[string]any) string {
	s, _ := u["status"].(string)
	return s
}
