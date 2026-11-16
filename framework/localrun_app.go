package framework

import (
	"net"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

// LocalRunInfo is what a host adds to this App's local run record
// (framework/localrun).
type LocalRunInfo struct {
	// Service is the battery/service Config.ID.
	Service string
	// MCPCommand is an argv that speaks stdio MCP to this run.
	MCPCommand []string
	// Dev marks a development run started outside `gofastr dev`.
	Dev bool
}

// SetLocalRunInfo sets the extra fields of this App's local run
// record. Call it before Start.
func (a *App) SetLocalRunInfo(info LocalRunInfo) {
	info.MCPCommand = append([]string(nil), info.MCPCommand...)
	a.localRunInfo = info
}

func (a *App) isLocalRun(host string) bool {
	if dev.Enabled() || a.localRunInfo.Dev || a.localRunInfo.Service != "" || len(a.localRunInfo.MCPCommand) > 0 {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *App) registerLocalRun(bound string) func() {
	if localrun.Disabled() {
		return func() {}
	}
	host, port, err := net.SplitHostPort(bound)
	if err != nil {
		return func() {}
	}
	forced := localrun.Forced()
	if !forced && !a.isLocalRun(host) {
		return func() {}
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	rec := localrun.Record{
		Name:       a.Config.Name,
		URL:        a.urlScheme() + "://" + net.JoinHostPort(host, port),
		Service:    a.localRunInfo.Service,
		MCPCommand: a.localRunInfo.MCPCommand,
		Dev:        dev.Enabled() || a.localRunInfo.Dev,
	}
	if a.mcpAutoMount || a.routerHasMCPRoute() {
		rec.MCPPath = "/mcp"
	}
	if exe, err := os.Executable(); err == nil {
		rec.Exe = exe
		if rec.Name == "" {
			rec.Name = filepath.Base(exe)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		rec.Dir = wd
	}
	unregister, err := localrun.Register(rec)
	if err != nil {
		log := a.Logger().Debug
		if forced {
			log = a.Logger().Warn
		}
		log("local run registry: record not written", "error", err.Error(),
			"opt_out", localrun.EnvRuns+"=off")
		return func() {}
	}
	a.Logger().Debug("local run registry: recorded", "url", rec.URL)
	return unregister
}
