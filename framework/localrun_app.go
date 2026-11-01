package framework

import (
	"net"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/framework/dev"
	"github.com/DonaldMurillo/gofastr/framework/localrun"
)

// LocalRunInfo is what a host adds to this App's local run record
// (framework/localrun) beyond what the App knows itself.
type LocalRunInfo struct {
	// Service is the battery/service Config.ID.
	Service string
	// MCPCommand is an argv that speaks stdio MCP to this run.
	MCPCommand []string
	// Dev marks a development run started outside `gofastr dev` (a
	// service battery's foreground run).
	Dev bool
}

// SetLocalRunInfo sets the extra fields of this App's local run
// record. Call it before Start; battery/service does.
func (a *App) SetLocalRunInfo(info LocalRunInfo) {
	info.MCPCommand = append([]string(nil), info.MCPCommand...)
	a.localRunInfo = info
}

// isLocalRun reports whether this run is one a tool on the same machine
// looks for: under `gofastr dev`, a host-marked run (battery/service),
// or a listener on a loopback address (host is the bound IP).
func (a *App) isLocalRun(host string) bool {
	if dev.Enabled() || a.localRunInfo.Dev || a.localRunInfo.Service != "" || len(a.localRunInfo.MCPCommand) > 0 {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// registerLocalRun writes the run record for the bound listener and
// returns the func that removes it. A failure is logged and serving
// goes on: the registry is a convenience for local tools, never a
// reason to refuse to start.
//
// Unless the environment forces it (localrun.Forced), only a local run
// registers: a dev run, a battery/service run, or a listener on a
// loopback address. A server bound to every interface is a deployment
// (a container, a VM), where the registry has no reader and the home
// directory is often missing or read-only (#635). A failure of an
// automatic registration is logged at debug level; a forced one warns.
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
