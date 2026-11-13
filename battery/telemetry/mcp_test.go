package telemetry_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/core/handler"
)

type adminCaller struct{}

func (adminCaller) GetRoles() []string { return []string{"admin"} }

type plainCaller struct{}

func (plainCaller) GetRoles() []string { return []string{"user"} }

func admin() context.Context { return handler.SetUser(context.Background(), adminCaller{}) }

func toolNames(a appUnderTest) []string {
	var names []string
	for _, tool := range a.app.MCP.ListTools() {
		if strings.HasPrefix(tool.Name, "telemetry_") {
			names = append(names, tool.Name)
		}
	}
	return names
}

func call(t *testing.T, a appUnderTest, ctx context.Context, tool string, params map[string]any) map[string]any {
	t.Helper()
	res, err := a.app.MCP.CallTool(ctx, tool, params)
	if err != nil {
		t.Fatalf("%s(%v): %v", tool, params, err)
	}
	// Through JSON, as a client receives it.
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("%s: result does not encode: %v", tool, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMCPToolsAreOptIn(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	if names := toolNames(start(t, telemetry.Config{})); len(names) != 0 {
		t.Fatalf("tools registered without EnableMCP: %v", names)
	}
	if names := toolNames(start(t, telemetry.Config{EnableMCP: true})); len(names) != 3 {
		t.Fatalf("with EnableMCP: %v, want the three tools", names)
	}
}

// The tools hand out request paths, client addresses and log lines: by
// default only an admin calls them, and a custom gate replaces that.
func TestMCPToolsRefuseNonAdmins(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	a := start(t, telemetry.Config{EnableMCP: true, SampleRate: 1})
	a.get("/orders/42")
	for _, tool := range []string{"telemetry_requests", "telemetry_request", "telemetry_logs"} {
		params := map[string]any{"trace_id": strings.Repeat("a", 32)}
		for name, ctx := range map[string]context.Context{
			"no caller":  context.Background(),
			"plain user": handler.SetUser(context.Background(), plainCaller{}),
		} {
			if res, err := a.app.MCP.CallTool(ctx, tool, params); err == nil {
				t.Errorf("%s by %s: answered %v", tool, name, res)
			}
		}
		if _, err := a.app.MCP.CallTool(admin(), tool, params); err != nil {
			t.Errorf("%s by an admin: %v", tool, err)
		}
	}

	open := start(t, telemetry.Config{EnableMCP: true, MCPGate: func(context.Context) error { return nil }})
	if _, err := open.app.MCP.CallTool(context.Background(), "telemetry_logs", nil); err != nil {
		t.Errorf("a custom gate that admits everyone still refused: %v", err)
	}
}

func TestMCPToolsReadTheFiles(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	a := start(t, telemetry.Config{EnableMCP: true, SampleRate: 1})
	a.get("/orders/42")
	a.get("/orders/boom")

	all := call(t, a, admin(), "telemetry_requests", nil)
	failed := call(t, a, admin(), "telemetry_requests", map[string]any{"failed": true})
	if all["count"] != 2.0 || failed["count"] != 1.0 {
		t.Fatalf("requests: %v listed, %v failed; want 2 and 1", all["count"], failed["count"])
	}
	req := failed["requests"].([]any)[0].(map[string]any)
	id, _ := req["trace_id"].(string)
	if len(id) != 32 || req["status"] != 500.0 || req["route"] != "GET /orders/{id}" || req["path"] != "/orders/boom" || req["error"] != true {
		t.Fatalf("the failed request: %v", req)
	}
	if one := call(t, a, admin(), "telemetry_requests", map[string]any{"limit": 1}); one["count"] != 1.0 {
		t.Fatalf("limit 1 returned %v", one["count"])
	}
	if slow := call(t, a, admin(), "telemetry_requests", map[string]any{"slow": true}); slow["count"] != 0.0 {
		t.Fatalf("slow listed %v requests that took well under the threshold", slow["count"])
	}
	if byRoute := call(t, a, admin(), "telemetry_requests", map[string]any{"route": "GET /nowhere"}); byRoute["count"] != 0.0 {
		t.Fatalf("an unknown route returned %v requests", byRoute["count"])
	}

	got := call(t, a, admin(), "telemetry_request", map[string]any{"trace_id": id})
	spans, _ := got["request"].(map[string]any)["spans"].([]any)
	if got["found"] != true || len(spans) != 2 || len(got["logs"].([]any)) != 2 {
		t.Fatalf("telemetry_request: found=%v, %d spans, logs %v", got["found"], len(spans), got["logs"])
	}
	if miss := call(t, a, admin(), "telemetry_request", map[string]any{"trace_id": strings.Repeat("0", 32)}); miss["found"] != false {
		t.Fatalf("an unknown trace: %v", miss)
	}

	logs := call(t, a, admin(), "telemetry_logs", map[string]any{"text": "order.loaded"})
	if logs["count"] != 2.0 || logs["entries"].([]any)[0].(map[string]any)["msg"] != "order.loaded" {
		t.Fatalf("telemetry_logs text filter: %v", logs)
	}
	if errs := call(t, a, admin(), "telemetry_logs", map[string]any{"level": "ERROR"}); errs["count"] != 0.0 {
		t.Fatalf("level ERROR returned %v lines from an app that logged none", errs["count"])
	}
}

// An argument that cannot be read is an error, never a filter quietly
// dropped: the caller would take everything for the answer to a
// narrower question.
func TestMCPToolsRefuseUnreadableArguments(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	a := start(t, telemetry.Config{EnableMCP: true})
	for name, c := range map[string]struct {
		tool   string
		params map[string]any
	}{
		"failed as a string": {"telemetry_requests", map[string]any{"failed": "yes"}},
		"route as a number":  {"telemetry_requests", map[string]any{"route": 7}},
		"route too long":     {"telemetry_requests", map[string]any{"route": strings.Repeat("r", 2000)}},
		"limit as a string":  {"telemetry_requests", map[string]any{"limit": "10"}},
		"limit zero":         {"telemetry_requests", map[string]any{"limit": 0}},
		"limit a fraction":   {"telemetry_logs", map[string]any{"limit": 1.5}},
		"since not a time":   {"telemetry_logs", map[string]any{"since_ts": "yesterday"}},
		"level unknown":      {"telemetry_logs", map[string]any{"level": "LOUD"}},
		"trace_id not hex":   {"telemetry_request", map[string]any{"trace_id": "not-an-id"}},
		// A tool takes the ID exactly as the files hold it.
		"trace_id padded":      {"telemetry_request", map[string]any{"trace_id": " " + strings.Repeat("a", 32) + " "}},
		"trace_id upper case":  {"telemetry_request", map[string]any{"trace_id": strings.Repeat("A", 32)}},
		"trace_id as a number": {"telemetry_request", map[string]any{"trace_id": 12}},
	} {
		if res, err := a.app.MCP.CallTool(admin(), c.tool, c.params); err == nil {
			t.Errorf("%s: answered %v", name, res)
		}
	}
}
