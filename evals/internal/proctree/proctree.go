// Package proctree owns the child processes an eval harness spawns:
// binary naming, port allocation, and teardown of whole process trees.
package proctree

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// ExecutableName appends .exe on Windows so a built binary launches.
func ExecutableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// FreePort reserves an ephemeral loopback TCP port for a child server.
func FreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// Command owns a started command and its process group or job, when available.
type Command struct {
	tree commandTree
	cmd  *exec.Cmd
	once sync.Once
	done chan struct{}
	err  error
}

// Start starts a fresh exec.Command and takes sole responsibility for waiting.
// Context cancellation stops the owned tree.
func Start(ctx context.Context, cmd *exec.Cmd) (*Command, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cmd.Process != nil {
		return nil, errors.New("command already started")
	}
	cmd.WaitDelay = 5 * time.Second
	tree, startErr := startCommand(cmd)
	if cmd.Process == nil {
		return nil, startErr
	}
	p := &Command{tree: tree, cmd: cmd, done: make(chan struct{})}
	go p.wait(ctx, startErr)
	if startErr != nil {
		return nil, p.Wait()
	}
	return p, nil
}

// Run starts an owned command and waits for teardown and exit.
func Run(ctx context.Context, cmd *exec.Cmd) error {
	p, err := Start(ctx, cmd)
	if err != nil {
		return err
	}
	return p.Wait()
}

func (p *Command) wait(ctx context.Context, startErr error) {
	defer close(p.done)
	cancelStop := context.AfterFunc(ctx, p.terminate)
	defer cancelStop()
	if observeBeforeReap {
		if startErr == nil {
			startErr = observeExit(p.cmd.Process)
		}
		p.terminate()
	}
	waitErr := p.cmd.Wait()
	if !observeBeforeReap {
		p.terminate()
	}
	if startErr != nil {
		p.err = startErr
		return
	}
	p.err = waitErr
}

func (p *Command) terminate() {
	p.once.Do(func() { p.tree.stop(p.cmd.Process) })
}

// Done is closed after the command exits and teardown finishes.
func (p *Command) Done() <-chan struct{} { return p.done }

// Wait returns the command's final error after teardown.
func (p *Command) Wait() error {
	<-p.done
	return p.err
}

// Stop terminates the owned tree and waits for teardown.
func (p *Command) Stop() {
	p.terminate()
	<-p.done
}
