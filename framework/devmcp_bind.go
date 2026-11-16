package framework

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/config"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

const devMCPExposeEnv = "GOFASTR_DEV_MCP_EXPOSE"

func bindIsLocal(addr, network string) bool {
	if inProcessNetwork(network) {
		return true
	}
	return tcpNetwork(network) && bindIsLoopback(addr)
}

func bindIsLoopback(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	} else if !strings.Contains(addr, ":") {
		if _, convErr := strconv.Atoi(addr); convErr == nil {
			return false
		}
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func listenAddrFor(addr string) string {
	if addr == "" {
		return ":http"
	}
	return addr
}

func devMCPExposeAllowed() bool { return config.EnvBool(devMCPExposeEnv) }

func devMCPExposureWarning(addr string) string {
	return fmt.Sprintf(
		"gofastr dev: refusing to expose the MCP control surface on %s: it is not a loopback bind, "+
			"and the dev MCP has no authentication in front of its MUTATING tools. "+
			"The transport's loopback Host pin stops DNS rebinding from a browser but not a direct TCP client, "+
			"which sets Host freely. Bind to localhost, or set %s=1 to accept the risk.",
		addr, devMCPExposeEnv)
}

func (a *App) guardDevMCPBind(addr, network string) {
	if bindIsLocal(addr, network) {
		return
	}
	if devMCPExposeAllowed() {
		if a.MCP != nil {
			a.MCP.SetRequireLoopbackHost(false)
		}
		return
	}
	withdrew := false
	if a.mcpControlDevImplied && a.mcpControl {
		a.mcpControl = false
		if a.MCP != nil {
			for _, name := range a.controlToolNames() {
				a.MCP.UnregisterTool(name)
			}
		}
		withdrew = true
	}
	if a.MCP != nil && len(a.MCP.UnregisterDevImpliedTools()) > 0 {
		withdrew = true
	}
	if !withdrew {
		return
	}
	a.Logger().Warn(devMCPExposureWarning(addr), "addr", addr)
}

func (a *App) guardDevLiveReloadBind(addr, network string) {
	if bindIsLocal(addr, network) || config.EnvBool(dev.LiveReloadExposeEnv) {
		return
	}
	switch dev.WithdrawLiveReload(a.router) {
	case dev.Withdrawn:
		a.Logger().Warn(fmt.Sprintf(
			"gofastr dev: livereload is off on %s: it is not a loopback bind, and the stream has no limit on how many clients hold it open. "+
				"Bind to localhost, or set %s=1 to keep it.", addr, dev.LiveReloadExposeEnv), "addr", addr)
	case dev.HostOwned:
		a.Logger().Warn(fmt.Sprintf(
			"gofastr dev: %s is not a loopback bind, and the app serves its own %s, which stays on. "+
				"Turn it off on such a bind, or bind to localhost.", addr, dev.LiveReloadStreamURL), "addr", addr)
	}
}
