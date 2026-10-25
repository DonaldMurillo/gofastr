package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// devMCPApp builds the app shape `gofastr dev` runs: GOFASTR_DEV=1, one CRUD
// entity whose MCP exposure is NOT opted in (only the dev loop implies it),
// and battery/log for the dev-implied log tools.
func devMCPApp(t *testing.T) *framework.App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1) // modernc :memory: is per-connection; keep one
	t.Cleanup(func() { _ = db.Close() })
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "devmcp-red"}), framework.WithDB(db))
	crudTrue := true
	app.Entity("notes", entity.EntityConfig{
		Table: "notes",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
		},
		Exposure: &entity.ExposureConfig{CRUD: &crudTrue},
	}.WithTimestamps(false))
	app.RegisterBattery(&log.Plugin{})
	return app
}

type devMCPClient struct {
	base string
}

func (c devMCPClient) call(t *testing.T, id int, method, params string) mcp.Response {
	t.Helper()
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, params)
	req, err := http.NewRequest(http.MethodPost, c.base+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The transport pins the Host header to loopback; a direct TCP client
	// sets it freely. This is the pinned-test bypass, restated.
	req.Host = "localhost"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp: status %d", resp.StatusCode)
	}
	var out mcp.Response
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return out
}

func (c devMCPClient) listedTools(t *testing.T) map[string]bool {
	t.Helper()
	res := c.call(t, 1, "tools/list", `{}`)
	blob, err := json.Marshal(res.Result)
	if err != nil {
		t.Fatalf("marshal tools/list result: %v", err)
	}
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(blob, &listed); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	names := make(map[string]bool, len(listed.Tools))
	for _, tl := range listed.Tools {
		names[tl.Name] = true
	}
	return names
}

// startDevMCPApp boots app on bindAddr and returns a client pointed at the
// bound address, plus a shutdown func.
func startDevMCPApp(t *testing.T, app *framework.App, bindAddr string) (devMCPClient, func()) {
	t.Helper()
	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	started := make(chan error, 1)
	go func() { started <- app.Start(bindAddr) }()

	var base string
	select {
	case addr := <-ready:
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("ready addr %q: %v", addr, err)
		}
		base = "http://127.0.0.1:" + port
	case err := <-started:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("Start never became ready")
	}
	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
		select {
		case err := <-started:
			if err != nil {
				t.Errorf("Start: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Start did not return after Shutdown")
		}
	}
	return devMCPClient{base: base}, shutdown
}

func devMCPNotesCount(t *testing.T, app *framework.App) int {
	t.Helper()
	var n int
	if err := app.DB.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	return n
}

// TestDevMCPExposedBindDropsWrites pins that a non-loopback bind under
// GOFASTR_DEV withdraws every dev-implied MUTATING and disclosing MCP
// surface. A loopback bind of the identical app must keep them (the dev
// loop's whole point), so the withdrawal is attributable to the bind, not
// to the fixture.
func TestDevMCPExposedBindDropsWrites(t *testing.T) {
	t.Setenv("GOFASTR_DOTENV", "off")
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV_MCP", "")
	t.Setenv("GOFASTR_DEV_MCP_EXPOSE", "")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	mutating := []string{"notes_create", "notes_update", "notes_delete", "log_set_level"}

	// Phase 1 (specificity guard): loopback bind serves the dev-implied
	// mutating tools. If this fails, the fixture is wrong, not the guard.
	loopApp := devMCPApp(t)
	loopClient, loopStop := startDevMCPApp(t, loopApp, "127.0.0.1:0")
	defer loopStop()
	listed := loopClient.listedTools(t)
	for _, name := range mutating {
		if !listed[name] {
			t.Fatalf("fixture check: loopback dev bind must list %q (dev-implied surface); got tools %v", name, listed)
		}
	}

	// Phase 2 (the finding): the same app on a non-loopback bind. The
	// withdrawn set is the whole dev-implied mutating/disclosing surface:
	// the entity writes, battery/log's set_level, the two disclosing log
	// reads, and the disk-writing contract tool.
	app := devMCPApp(t)
	client, stop := startDevMCPApp(t, app, "0.0.0.0:0")
	defer stop()

	withdrawn := append(append([]string{}, mutating...),
		"log_recent", "log_filter", "contracts_fix")
	listed = client.listedTools(t)
	for _, name := range withdrawn {
		if listed[name] {
			t.Errorf("SECURITY: [exposure] anonymous tools/list on an exposed dev bind disclosed dev-implied mutating tool %q", name)
		}
	}

	// Reach proof: log_set_level registers directly on the MCP server (its
	// own doc: "no route middleware ever runs for it") and its gate is nil
	// under the dev implication — so this call must NOT succeed on an
	// exposed bind. It answers with .previous_level on success.
	res := client.call(t, 2, "tools/call", `{"name":"log_set_level","arguments":{"level":"ERROR"}}`)
	if res.Error == nil {
		blob, _ := json.Marshal(res.Result)
		t.Errorf("SECURITY: [exposure] anonymous tools/call log_set_level succeeded on an exposed dev bind (went quiet for the attacker): %s", blob)
	}
	if n := devMCPNotesCount(t, app); n != 0 {
		t.Errorf("SECURITY: [exposure] anonymous tools/call notes_create on an exposed dev bind inserted a row (count=%d)", n)
	}
}
