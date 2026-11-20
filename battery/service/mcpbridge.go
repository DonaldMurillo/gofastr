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
	"github.com/DonaldMurillo/gofastr/framework/localapp"
)

func mcpSession(srv *mcp.Server, user *localapp.User) func(context.Context, io.Reader, io.Writer) {
	return func(ctx context.Context, in io.Reader, out io.Writer) {
		if user != nil {
			ctx = handler.SetUser(ctx, user)
		}
		_ = srv.ServeStdio(ctx, in, out)
	}
}

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

func (b *Battery) mcpBridge(dev bool, stateDir string, in io.Reader, out io.Writer) error {
	var l layout
	if stateDir != "" {
		if !filepath.IsAbs(stateDir) {
			return fmt.Errorf("service: --state-dir must be absolute, got %q", stateDir)
		}
		l = layout{dir: filepath.Clean(stateDir)}
	} else {
		var err error
		if l, err = b.layoutFor(dev); err != nil {
			return err
		}
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
		if uc, ok := c.(*net.UnixConn); !ok || uc.CloseWrite() != nil {
			c.Close()
		}
	}()
	if _, err := io.Copy(out, br); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("service: mcp session: %w", err)
	}
	return nil
}
