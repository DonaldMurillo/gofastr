package acp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/gofastr/gofastr/kiln/agent/acp"
	"github.com/gofastr/gofastr/kiln/db"
	"github.com/gofastr/gofastr/kiln/journal"
	"github.com/gofastr/gofastr/kiln/live"
	"github.com/gofastr/gofastr/kiln/protocol"
	"github.com/gofastr/gofastr/framework"
)

func setup(t *testing.T) *protocol.Tools {
	t.Helper()
	d, cleanup, err := db.EphemeralSQLite("kiln-acp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	factory := func() *framework.App { return framework.NewApp(framework.WithDB(d)) }
	l, err := live.New(journal.NewMemory(), factory)
	if err != nil {
		t.Fatal(err)
	}
	return protocol.New(l)
}

func sendRPC(t *testing.T, srv *acp.Server, method string, params any) map[string]any {
	t.Helper()
	in := &bytes.Buffer{}
	enc := json.NewEncoder(in)
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	if err := enc.Encode(body); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	if err := srv.Serve(context.Background(), in, out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, out.String())
	}
	return resp
}

func TestInitialize(t *testing.T) {
	srv := acp.New(setup(t))
	resp := sendRPC(t, srv, "initialize", nil)
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result: %v", resp)
	}
	if res["protocol_version"] == nil {
		t.Errorf("missing protocol_version: %v", res)
	}
}

func TestToolsList(t *testing.T) {
	srv := acp.New(setup(t))
	resp := sendRPC(t, srv, "tools/list", nil)
	res, _ := resp["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	if len(tools) == 0 {
		t.Fatalf("no tools: %v", resp)
	}
	got := tools[0].(map[string]any)
	if got["name"] == nil {
		t.Errorf("missing tool name: %v", got)
	}
}

func TestToolsCallAddEntity(t *testing.T) {
	tools := setup(t)
	srv := acp.New(tools)
	resp := sendRPC(t, srv, "tools/call", map[string]any{
		"name": "add_entity",
		"arguments": map[string]any{
			"entity": map[string]any{
				"name":   "posts",
				"fields": []any{map[string]any{"name": "title", "type": "string"}},
			},
		},
	})
	res, _ := resp["result"].(map[string]any)
	if res == nil || res["ok"] != true {
		t.Fatalf("expected ok result, got %v", resp)
	}
	if _, ok := tools.Live().Session().World.Entities["posts"]; !ok {
		t.Error("posts not added")
	}
}

func TestToolsCallReturnsErrorKind(t *testing.T) {
	srv := acp.New(setup(t))
	resp := sendRPC(t, srv, "tools/call", map[string]any{
		"name":      "add_field",
		"arguments": map[string]any{"entity": "missing", "field": map[string]any{"name": "x", "type": "string"}},
	})
	res, _ := resp["result"].(map[string]any)
	if res == nil || res["ok"] != false || res["kind"] != "not_found" {
		t.Errorf("expected not_found result, got %v", resp)
	}
}

func TestPromptJournalsUserMessage(t *testing.T) {
	tools := setup(t)
	srv := acp.New(tools)
	resp := sendRPC(t, srv, "prompt", map[string]any{"text": "hello"})
	res, _ := resp["result"].(map[string]any)
	if res == nil || res["ok"] != true {
		t.Fatalf("expected ok, got %v", resp)
	}
	chat := tools.Live().Session().Chat
	if len(chat) != 1 || chat[0].Message == nil || chat[0].Message.Text != "hello" {
		t.Errorf("user chat not journaled: %+v", chat)
	}
}

func TestUnknownMethodErrors(t *testing.T) {
	srv := acp.New(setup(t))
	resp := sendRPC(t, srv, "not_a_real_method", nil)
	errObj, _ := resp["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error, got %v", resp)
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "method not found") {
		t.Errorf("error msg = %q", msg)
	}
}

func TestPromptStringIncludesPersona(t *testing.T) {
	srv := acp.New(setup(t))
	s := srv.PromptString()
	if !strings.Contains(s, "Kiln") {
		t.Errorf("prompt string missing 'Kiln': %s", s)
	}
}
