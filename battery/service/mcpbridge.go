package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/internal/localuser"
)

// mcpSession serves one stdio MCP session against the App's MCP
// server. The session runs as the local identity the gate gives a
// signed-in browser: the control socket already proved the caller is
// the OS user the daemon runs as.
func mcpSession(srv *mcp.Server, user *localuser.User) func(context.Context, io.Reader, io.Writer) {
	return func(ctx context.Context, in io.Reader, out io.Writer) {
		if user != nil {
			ctx = handler.SetUser(ctx, user)
		}
		_ = srv.ServeStdio(ctx, in, out)
	}
}

// mcpCommand is the argv that bridges an agent to the daemon whose
// state directory is dir: `<exe> service mcp [--dev] --state-dir
// <dir>`. The directory is spelled out because an agent starts the
// command from its own working directory and environment, which pick
// neither a worktree's dev run nor a GOFASTR_SERVICE_DATA_DIR base.
// Nil when the executable path is unknown.
func mcpCommand(dev bool, dir string) []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	argv := []string{exe, "service", "mcp"}
	if dev {
		argv = append(argv, "--dev")
	}
	return append(argv, "--state-dir", dir)
}

// mcpBridge is `service mcp`: it connects to the running daemon's
// control socket and copies stdin to the daemon's MCP server and its
// replies to stdout, so an agent configured with this command as a
// stdio MCP server reaches the app with no token: the socket admits
// only the same OS user. stateDir, when set, names the daemon's state
// directory instead of resolving it from dev, the working directory and
// the environment.
func (b *Battery) mcpBridge(dev bool, stateDir string, in io.Reader, out io.Writer) error {
	l, err := b.layoutFor(dev)
	if err != nil {
		return err
	}
	if stateDir != "" {
		if !filepath.IsAbs(stateDir) {
			return fmt.Errorf("service: --state-dir must be absolute, got %q", stateDir)
		}
		l = layout{dir: filepath.Clean(stateDir)}
	}
	c, br, _, err := openControl(l.socketPath(), "mcp")
	if errors.Is(err, errNotRunning) {
		hint := b.cfg.Name + " service start"
		if dev {
			hint = b.cfg.Name + " service run"
		}
		return fmt.Errorf("service: %s is not running; start it with `%s`", b.cfg.Name, hint)
	}
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Time{})

	go func() {
		_, _ = io.Copy(c, in)
		// End of input ends the session: half-close so the daemon
		// sees EOF and still flushes its last replies.
		if uc, ok := c.(*net.UnixConn); !ok || uc.CloseWrite() != nil {
			c.Close()
		}
	}()
	if _, err := io.Copy(out, br); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("service: mcp session: %w", err)
	}
	return nil
}
