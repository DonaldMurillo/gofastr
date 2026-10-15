package framework

import (
	"bytes"
	"context"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

// Start writes a local run record once the port is held, and the
// record is gone once Shutdown stops serving. A wildcard bind is
// recorded as a loopback URL a local client can dial.
func TestStartRecordsLocalRun(t *testing.T) {
	t.Setenv(localrun.EnvDir, t.TempDir())
	app := NewApp(WithoutDefaultMiddleware(), WithConfig(AppConfig{Name: "probe"}))
	app.SetLocalRunInfo(LocalRunInfo{Service: "dev.example.probe", MCPCommand: []string{"/bin/probe", "service", "mcp"}, Dev: true})
	addr, stop := serveForRecord(t, app, ":0")
	_, port, _ := net.SplitHostPort(addr)

	recs, err := localrun.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("want one record while serving, got %+v", recs)
	}
	r := recs[0]
	if r.Name != "probe" || r.URL != "http://127.0.0.1:"+port || r.Service != "dev.example.probe" ||
		!slices.Equal(r.MCPCommand, []string{"/bin/probe", "service", "mcp"}) || !r.Dev || r.Exe == "" || r.Dir == "" {
		t.Fatalf("record = %+v", r)
	}
	if r.MCPPath != "" {
		t.Fatalf("an app with no /mcp recorded MCPPath %q", r.MCPPath)
	}

	stop()
	if recs, _ := localrun.List(); len(recs) != 0 {
		t.Fatalf("record outlived Shutdown: %+v", recs)
	}
}

// An app serving /mcp says so in its record.
func TestLocalRunRecordsMCPPath(t *testing.T) {
	t.Setenv(localrun.EnvDir, t.TempDir())
	app := NewApp(WithoutDefaultMiddleware(), WithMCP())
	_, stop := serveForRecord(t, app, "127.0.0.1:0")
	defer stop()
	recs, _ := localrun.List()
	if len(recs) != 1 || recs[0].MCPPath != "/mcp" || recs[0].MCPURL() != recs[0].URL+"/mcp" {
		t.Fatalf("records = %+v", recs)
	}
}

// GOFASTR_RUNS=off keeps a run out of the registry.
func TestLocalRunOptOut(t *testing.T) {
	t.Setenv(localrun.EnvDir, t.TempDir())
	t.Setenv(localrun.EnvOff, "off")
	app := NewApp(WithoutDefaultMiddleware())
	_, stop := serveForRecord(t, app, "127.0.0.1:0")
	defer stop()
	if recs, _ := localrun.List(); len(recs) != 0 {
		t.Fatalf("opted-out run registered: %+v", recs)
	}
}

// serveForRecord starts app on addr and returns the bound address and
// a stop func that shuts it down and waits for Start to return.
func serveForRecord(t *testing.T, app *App, addr string) (string, func()) {
	t.Helper()
	app.startupOutput = &bytes.Buffer{}
	ready := make(chan string, 1)
	app.OnReady(func(a string) { ready <- a })
	done := make(chan error, 1)
	go func() { done <- app.Start(addr) }()
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
		<-done
	}
	select {
	case a := <-ready:
		return a, stop
	case err := <-done:
		t.Fatalf("Start returned early: %v", err)
	case <-time.After(5 * time.Second):
		stop()
		t.Fatal("OnReady never fired")
	}
	return "", nil
}
