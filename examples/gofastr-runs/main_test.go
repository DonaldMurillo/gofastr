package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/service"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

// The App is exercised the way the daemon builds it, minus the
// listener: buildApp with a Runtime whose state dir is a temp dir, and
// TestHarness through the real router. The loopback gate, the sign-in
// flow and the `service mcp` bridge are battery/service's own tests.

type localUser struct{ id string }

func (u localUser) GetID() string { return u.id }

// twoRuns registers a service run (stdio MCP) and a dev run (HTTP MCP)
// in a temp registry and returns their ids.
func twoRuns(t *testing.T) (svc, dev string) {
	t.Helper()
	t.Setenv(localrun.EnvDir, t.TempDir())
	for _, rec := range []localrun.Record{
		{Name: "billing", URL: "http://127.0.0.1:47900", Service: "dev.acme.billing", MCPCommand: []string{"/opt/billing", "service", "mcp"}},
		{Name: "Acme Site", URL: "http://127.0.0.1:8082", MCPPath: "/mcp", Dev: true},
	} {
		unregister, err := localrun.Register(rec)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(unregister)
	}
	recs, err := localrun.List()
	if err != nil || len(recs) != 2 {
		t.Fatalf("registry = %v, %v", recs, err)
	}
	for _, r := range recs {
		if r.Service != "" {
			svc = r.ID
		} else {
			dev = r.ID
		}
	}
	return svc, dev
}

// callTool runs one tools/call and decodes its JSON text content.
func callTool(t *testing.T, s *mcp.Server, name string, args map[string]any, out any) *mcp.RPCError {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	resp := s.HandleRequest(context.Background(), mcp.Request{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: params})
	if resp.Error != nil {
		return resp.Error
	}
	b, _ := json.Marshal(resp.Result)
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(b, &result); err != nil || len(result.Content) == 0 {
		t.Fatalf("%s result = %s", name, b)
	}
	if result.IsError {
		return &mcp.RPCError{Message: result.Content[0].Text}
	}
	if err := json.Unmarshal([]byte(result.Content[0].Text), out); err != nil {
		t.Fatalf("%s content = %s: %v", name, result.Content[0].Text, err)
	}
	return nil
}

func TestToolsReportRunsAndHowToConnect(t *testing.T) {
	svcID, devID := twoRuns(t)
	s := mcp.NewServer()
	if err := registerTools(s); err != nil {
		t.Fatal(err)
	}

	var list struct{ Runs []run }
	if err := callTool(t, s, "runs_list", nil, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Runs) != 2 {
		t.Fatalf("runs = %+v", list.Runs)
	}
	for _, r := range list.Runs {
		switch r.ID {
		case svcID:
			if r.Kind != "service" || r.MCP == nil || r.MCP.Type != "stdio" || r.MCP.Command != "/opt/billing" ||
				strings.Join(r.MCP.Args, " ") != "service mcp" {
				t.Fatalf("service run = %+v (mcp %+v)", r, r.MCP)
			}
		case devID:
			if r.Kind != "dev" || r.MCP == nil || r.MCP.Type != "http" || r.MCP.URL != "http://127.0.0.1:8082/mcp" {
				t.Fatalf("dev run = %+v (mcp %+v)", r, r.MCP)
			}
		default:
			t.Fatalf("unknown run %q", r.ID)
		}
	}

	if err := callTool(t, s, "runs_list", map[string]any{"name": "ACME"}, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Runs) != 1 || list.Runs[0].ID != devID {
		t.Fatalf("name filter = %+v", list.Runs)
	}

	var one run
	if err := callTool(t, s, "run_get", map[string]any{"id": svcID}, &one); err != nil || one.Service != "dev.acme.billing" {
		t.Fatalf("run_get = %+v, %v", one, err)
	}
	if err := callTool(t, s, "run_get", map[string]any{"id": "1-00000000"}, &one); err == nil || !strings.Contains(err.Message, "no running app") {
		t.Fatalf("run_get of a stopped run = %v", err)
	}
}

func newTestApp(t *testing.T) *framework.App {
	t.Helper()
	app, err := buildApp(service.Runtime{DataDir: t.TempDir(), Secret: "gofastr-runs-test-secret-0123456789abcdef", Dev: true}) // not-a-secret: test fixture
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestScreensShowRuns(t *testing.T) {
	svcID, devID := twoRuns(t)
	ta := framework.TestHarness(t, newTestApp(t)).AsUser(localUser{id: "local"})

	list := ta.Get("/").AssertStatus(t, http.StatusOK).Body()
	for _, want := range []string{
		"billing", "Acme Site", `href="/runs/` + svcID + `"`, `href="/runs/` + devID + `"`,
		`data-cui-poll="5s"`, `data-cui-poll-src="` + tablePath + `"`,
		"&#34;mcpServers&#34;", "&#34;--dev&#34;",
	} {
		if !strings.Contains(list, want) && !strings.Contains(list, strings.ReplaceAll(want, "&#34;", "&quot;")) {
			t.Errorf("list screen lacks %q", want)
		}
	}
	if strings.Contains(list, "<style") {
		t.Error("bespoke inline CSS on the list screen")
	}

	frag := ta.Get(tablePath).AssertStatus(t, http.StatusOK)
	if body := frag.Body(); !strings.Contains(body, "billing") || strings.Contains(body, "<html") {
		t.Fatalf("table fragment = %s", body)
	}

	detail := ta.Get("/runs/"+svcID).AssertStatus(t, http.StatusOK).Body()
	for _, want := range []string{"dev.acme.billing", "/opt/billing", "http://127.0.0.1:47900"} {
		if !strings.Contains(detail, want) {
			t.Errorf("run screen lacks %q", want)
		}
	}
	if gone := ta.Get("/runs/1-00000000").AssertStatus(t, http.StatusOK).Body(); !strings.Contains(gone, "This run is not running") {
		t.Error("a stopped run's screen does not say so")
	}
}

func TestEmptyRegistry(t *testing.T) {
	t.Setenv(localrun.EnvDir, t.TempDir())
	ta := framework.TestHarness(t, newTestApp(t)).AsUser(localUser{id: "local"})
	if body := ta.Get(tablePath).AssertStatus(t, http.StatusOK).Body(); !strings.Contains(body, "No gofastr apps are running") {
		t.Fatalf("empty table = %s", body)
	}
}

func TestConfigKey(t *testing.T) {
	for in, want := range map[string]string{
		"Acme Site": "acme-site", "billing": "billing", "--": "gofastr-app", "": "gofastr-app", "a__b..c": "a-b-c",
	} {
		if got := configKey(in); got != want {
			t.Errorf("configKey(%q) = %q, want %q", in, got, want)
		}
	}
}
