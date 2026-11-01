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

// A server bound to every interface with nothing marking it local is a
// deployment: it writes no record unless the environment forces one
// (#635: a container user with no home directory logged a warning on
// every start).
func TestLocalRunSkipsWildcardDeployment(t *testing.T) {
	t.Setenv(localrun.EnvDir, "")
	t.Setenv(localrun.EnvRuns, "")
	t.Setenv("GOFASTR_DEV", "0") // a developer's shell must not make every run local
	t.Setenv("GOFASTR_ENV", "development")
	app := NewApp(WithoutDefaultMiddleware())
	if app.isLocalRun("0.0.0.0") || app.isLocalRun("::") || app.isLocalRun("192.168.1.20") {
		t.Fatal("a wildcard or LAN bind with no host mark must not count as a local run")
	}
	if !app.isLocalRun("127.0.0.1") || !app.isLocalRun("::1") {
		t.Fatal("a loopback bind is a local run")
	}
	// Each host mark makes a wildcard bind local on its own.
	for name, info := range map[string]LocalRunInfo{
		"service":     {Service: "dev.example.probe"},
		"dev":         {Dev: true},
		"mcp command": {MCPCommand: []string{"/bin/probe", "service", "mcp"}},
	} {
		marked := NewApp(WithoutDefaultMiddleware())
		marked.SetLocalRunInfo(info)
		if !marked.isLocalRun("0.0.0.0") {
			t.Errorf("%s: a host-marked run must register wherever it binds", name)
		}
	}
	t.Setenv("GOFASTR_DEV", "1")
	if !NewApp(WithoutDefaultMiddleware()).isLocalRun("0.0.0.0") {
		t.Fatal("a gofastr dev run must register wherever it binds")
	}
}

// The same decision through Start: a wildcard bind with nothing marking
// it local writes no record, though the registry directory is set (it
// says where, not whether).
func TestLocalRunWildcardStartWritesNothing(t *testing.T) {
	t.Setenv(localrun.EnvDir, t.TempDir())
	t.Setenv(localrun.EnvRuns, "")
	t.Setenv("GOFASTR_DEV", "0")
	app := NewApp(WithoutDefaultMiddleware())
	_, stop := serveForRecord(t, app, ":0")
	defer stop()
	if recs, _ := localrun.List(); len(recs) != 0 {
		t.Fatalf("a wildcard deployment registered: %+v", recs)
	}
}

// GOFASTR_RUNS=on registers a wildcard bind too.
func TestLocalRunForcedOn(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(localrun.EnvDir, dir)
	t.Setenv(localrun.EnvOff, "on")
	app := NewApp(WithoutDefaultMiddleware())
	_, stop := serveForRecord(t, app, ":0")
	defer stop()
	if recs, _ := localrun.List(); len(recs) != 1 {
		t.Fatalf("forced run did not register: %+v", recs)
	}
}
