package framework

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// devMCPExposeEnv is the explicit opt-in for serving the dev MCP surface
// on a non-loopback listener.
const devMCPExposeEnv = "GOFASTR_DEV_MCP_EXPOSE"

// bindIsLoopback reports whether a listen address will only accept
// connections from this machine.
//
// The empty host, ":8080", "8080", "", is NOT loopback: Go binds every
// interface for it, which is precisely the exposed case. A hostname that
// is not a loopback literal is treated as exposed without resolving it;
// a name that happens to resolve to 127.0.0.1 today can resolve
// elsewhere tomorrow, and this decides whether to publish an
// unauthenticated control plane.
func bindIsLoopback(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		// No address at all means the caller has not chosen yet; the
		// server-side default is loopback ("localhost:8080").
		return true
	}
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	} else if !strings.Contains(addr, ":") {
		// A bare port ("8080"). Go expands this to every interface.
		if _, convErr := strconv.Atoi(addr); convErr == nil {
			return false
		}
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false // ":8080": all interfaces
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// devMCPExposeAllowed reports whether the operator explicitly accepted
// serving the dev MCP surface off-loopback.
func devMCPExposeAllowed() bool {
	v := os.Getenv(devMCPExposeEnv)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// devMCPExposureWarning is the banner printed when dev mode declines to
// register its control tools because the listener is reachable.
func devMCPExposureWarning(addr string) string {
	return fmt.Sprintf(
		"gofastr dev: refusing to expose the MCP control surface on %s: it is not a loopback bind, "+
			"and the dev MCP has no authentication in front of its MUTATING tools. "+
			"The transport's loopback Host pin stops DNS rebinding from a browser but not a direct TCP client, "+
			"which sets Host freely. Bind to localhost, or set %s=1 to accept the risk.",
		addr, devMCPExposeEnv)
}

// guardDevMCPBind drops the dev-implied MCP control tools when the
// listener is not loopback. Called from Start once the real bind address
// is known. NewApp cannot know it.
//
// Only the DEV-IMPLIED opt-in is withdrawn. A host that asked for the
// control tools itself (WithMCPControl) keeps them: that is a deliberate
// production choice with its own gating, not the dev loop's convenience
// default.
func (a *App) guardDevMCPBind(addr string) {
	if !a.mcpControlDevImplied || !a.mcpControl {
		return
	}
	if bindIsLoopback(addr) || devMCPExposeAllowed() {
		return
	}
	a.mcpControl = false
	a.Logger().Warn(devMCPExposureWarning(addr), "addr", addr)
}
