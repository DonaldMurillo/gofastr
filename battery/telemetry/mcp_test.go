package telemetry_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/core/handler"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
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
	if names := toolNames(start(t, telemetry.Config{EnableMCP: true})); len(names) != 5 {
		t.Fatalf("with EnableMCP: %v, want the five tools", names)
	}
}

// The tools hand out request paths, client addresses and log lines: by
// default only an admin calls them, and a custom gate replaces that.
func TestMCPToolsRefuseNonAdmins(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	a := start(t, telemetry.Config{EnableMCP: true, SampleRate: 1})
	a.get("/orders/42")
	for _, tool := range []string{"telemetry_stats", "telemetry_issues", "telemetry_requests", "telemetry_request", "telemetry_logs"} {
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

	issues := call(t, a, admin(), "telemetry_issues", nil)
	list, _ := issues["issues"].([]any)
	if issues["count"] != 1.0 || len(list) != 1 || list[0].(map[string]any)["count"] != 1.0 || list[0].(map[string]any)["last_trace_id"] != id ||
		list[0].(map[string]any)["route"] != "GET /orders/{id}" || list[0].(map[string]any)["status"] != 500.0 {
		t.Fatalf("telemetry_issues: %v", issues)
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
		"failed as a string":      {"telemetry_requests", map[string]any{"failed": "yes"}},
		"route as a number":       {"telemetry_requests", map[string]any{"route": 7}},
		"route too long":          {"telemetry_requests", map[string]any{"route": strings.Repeat("r", 2000)}},
		"limit as a string":       {"telemetry_requests", map[string]any{"limit": "10"}},
		"limit zero":              {"telemetry_requests", map[string]any{"limit": 0}},
		"limit a fraction":        {"telemetry_logs", map[string]any{"limit": 1.5}},
		"since not a time":        {"telemetry_logs", map[string]any{"since_ts": "yesterday"}},
		"level unknown":           {"telemetry_logs", map[string]any{"level": "LOUD"}},
		"stats route as a number": {"telemetry_stats", map[string]any{"route": 7}},
		"stats since not a time":  {"telemetry_stats", map[string]any{"since_ts": "last week"}},
		"stats limit zero":        {"telemetry_stats", map[string]any{"limit": 0}},
		"stats window backwards":  {"telemetry_stats", map[string]any{"since_ts": "2026-11-13T00:00:00Z", "until_ts": "2026-11-12T00:00:00Z"}},
		"stats window empty":      {"telemetry_stats", map[string]any{"since_ts": "2026-11-13T00:00:00Z", "until_ts": "2026-11-13T00:00:00Z"}},
		"stats window of years":   {"telemetry_stats", map[string]any{"since_ts": "2020-01-01T00:00:00Z", "until_ts": "2026-11-13T00:00:00Z"}},
		"trace_id not hex":        {"telemetry_request", map[string]any{"trace_id": "not-an-id"}},
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

// telemetry_stats answers from the counts of every request: totals, by
// route, and a series short enough to read.
func TestMCPStatsCountEveryRequest(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	dir := t.TempDir()
	at := time.Now().Add(-10 * time.Minute)
	r := itel.NewRollup()
	for i := range 30 {
		r.Observe(at, "GET /orders/{id}", 7*time.Millisecond, false, false)
		if i < 6 {
			r.Observe(at, "POST /checkout", 800*time.Millisecond, i < 2, true)
		}
		if i < 4 {
			r.ObserveJob(at, "process welcome-email", 40*time.Millisecond, i == 0)
		}
	}
	// Written with the day's own stamp, as the plugin's flush does.
	if err := os.WriteFile(filepath.Join(dir, itel.FileName(itel.KindStats, time.Now())), r.Take(time.Now(), true), 0o600); err != nil {
		t.Fatal(err)
	}
	// And the machine, read twice: busier and fuller the first time.
	for ago, line := range map[time.Duration]string{
		9 * time.Minute: `"cpu":0.8000,"mem_used":3000000000,"mem_total":4294967296,"disk_used":1000,"disk_total":26843545600,"goroutines":50,"heap":1,"rss":2`,
		4 * time.Minute: `"cpu":0.2000,"mem_used":1000000000,"mem_total":4294967296,"disk_used":24696061952,"disk_total":26843545600,"goroutines":42,"heap":31457280,"rss":83886080`,
	} {
		at := time.Now().Add(-ago)
		f, err := os.OpenFile(filepath.Join(dir, itel.FileName(itel.KindHost, time.Now())), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(`{"time":"` + at.UTC().Format(time.RFC3339) + `",` + line + "}\n")
		if cerr := f.Close(); err != nil || cerr != nil {
			t.Fatal(err, cerr)
		}
	}
	a := start(t, telemetry.Config{EnableMCP: true, Dir: dir})

	got := call(t, a, admin(), "telemetry_stats", nil)
	m, _ := got["machine"].(map[string]any)
	if m["cpu"] != 0.2 || m["cpu_peak"] != 0.8 || m["mem_used"] != 1000000000.0 || m["mem_used_peak"] != 3000000000.0 ||
		m["disk_percent"] != 92.0 || m["goroutines"] != 42.0 || m["rss"] != 83886080.0 {
		t.Fatalf("machine: %v", m)
	}
	total, _ := got["total"].(map[string]any)
	if total["requests"] != 36.0 || total["failed"] != 2.0 || total["slow"] != 6.0 {
		t.Fatalf("total: %v", total)
	}
	work, _ := got["jobs"].([]any)
	if len(work) != 1 || got["jobs_not_listed"] != 0.0 {
		t.Fatalf("jobs: %v (not listed %v)", work, got["jobs_not_listed"])
	}
	if j := work[0].(map[string]any); j["name"] != "process welcome-email" || j["runs"] != 4.0 || j["failed"] != 1.0 || j["requests"] != nil || j["slow"] != nil {
		t.Fatalf("the job: %v", j)
	}
	routes, _ := got["routes"].([]any)
	if len(routes) != 2 || routes[0].(map[string]any)["route"] != "GET /orders/{id}" || routes[1].(map[string]any)["failed"] != 2.0 || got["routes_not_listed"] != 0.0 {
		t.Fatalf("routes: %v (not listed %v)", routes, got["routes_not_listed"])
	}
	// The last 24 hours in at most sixty whole-minute steps, and the
	// requests are in exactly one of them.
	series, _ := got["series"].([]any)
	step, _ := got["step_seconds"].(float64)
	if len(series) < 50 || len(series) > 61 || step < 60 || int(step)%60 != 0 {
		t.Fatalf("%d steps of %v seconds", len(series), step)
	}
	var sum float64
	for _, pt := range series {
		sum += pt.(map[string]any)["requests"].(float64)
	}
	if sum != 36 {
		t.Fatalf("the series adds up to %v, want 36", sum)
	}
	if got["slow_at_us"] != 500000.0 || got["duration_ceiling_us"] != 10000000.0 {
		t.Fatalf("slow_at_us %v, duration_ceiling_us %v", got["slow_at_us"], got["duration_ceiling_us"])
	}

	one := call(t, a, admin(), "telemetry_stats", map[string]any{"route": "POST /checkout", "limit": 1})
	if one["total"].(map[string]any)["requests"] != 6.0 || len(one["routes"].([]any)) != 1 {
		t.Fatalf("one route: %v", one)
	}
	capped := call(t, a, admin(), "telemetry_stats", map[string]any{"limit": 1})
	if len(capped["routes"].([]any)) != 1 || capped["routes_not_listed"] != 1.0 {
		t.Fatalf("limit 1: %v routes, %v not listed", len(capped["routes"].([]any)), capped["routes_not_listed"])
	}
	before := call(t, a, admin(), "telemetry_stats", map[string]any{"until_ts": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)})
	if before["machine"] != nil {
		t.Fatalf("a window with no reading has a machine: %v", before["machine"])
	}
	if before["total"].(map[string]any)["requests"] != 0.0 {
		t.Fatalf("a window that ends before the requests counted %v", before["total"])
	}
}
