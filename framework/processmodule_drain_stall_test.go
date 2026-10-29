package framework

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// stalledChild is a RunningChild that never reads its stdin, so a frame
// written to it blocks. Kill stands in for the process dying: the pipe
// loses its reader, which fails the blocked write, and Wait returns.
type stalledChild struct {
	stdinR    *io.PipeReader
	stdinW    *io.PipeWriter
	stdoutR   *io.PipeReader
	stdoutW   *io.PipeWriter
	codec     *moduleproto.Codec
	exited    chan struct{}
	killOnce  sync.Once
	killed    chan struct{}
	closeOnce sync.Once
	exitOnEOF bool
}

func newStalledChild(t *testing.T) *stalledChild {
	c := &stalledChild{exited: make(chan struct{}), killed: make(chan struct{})}
	c.stdinR, c.stdinW = io.Pipe()
	c.stdoutR, c.stdoutW = io.Pipe()
	codec, err := moduleproto.NewCodec(c.stdoutR, c.stdinW, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.codec = codec
	t.Cleanup(func() {
		_ = c.Kill()
		_ = c.stdoutW.Close()
	})
	return c
}

func (c *stalledChild) Codec() *moduleproto.Codec     { return c.codec }
func (c *stalledChild) Stderr() *moduleproto.RingSink { return nil }
func (c *stalledChild) Pid() int                      { return 1 }
func (c *stalledChild) ProcessGroup() int             { return 0 }

// CloseStdin does not close the write side here: a real child that
// forked a grandchild holding stdin keeps the pipe open past it, so only
// the kill can be relied on to end a blocked write. A child that reads
// (exitOnEOF) exits at EOF instead.
func (c *stalledChild) CloseStdin() error {
	if c.exitOnEOF {
		c.closeOnce.Do(func() { close(c.exited) })
	}
	return nil
}

func (c *stalledChild) Kill() error {
	c.killOnce.Do(func() {
		close(c.killed)
		_ = c.stdinR.CloseWithError(io.ErrClosedPipe)
		c.closeOnce.Do(func() { close(c.exited) })
	})
	return nil
}

func (c *stalledChild) Wait() error { <-c.exited; return nil }

// 2026-10-27 P2: the shutdown drain sent module.drain before it closed
// stdin or armed the kill, and the frame write ignores the call's
// context. A child that stopped reading stdin held the write, so the
// drain never reached CloseStdin or the kill and Shutdown hung.
func TestShutdownDrainKillsStalledChild(t *testing.T) {
	child := newStalledChild(t)
	peer := moduleproto.NewPeer(child.codec, moduleproto.RoleHost)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sl := &moduleSlot{name: "m", sup: sup, child: child, peer: peer}

	done := make(chan struct{})
	go func() {
		sup.drainOneForShutdown(sl, 100*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the shutdown drain hung on a child that stopped reading stdin")
	}
	select {
	case <-child.killed:
	default:
		t.Fatal("the drain returned without killing the stalled child")
	}
}

// TestShutdownDrainSparesExitingChild pins the other side: a child that
// reads module.drain and exits at stdin EOF inside its budget is not
// killed.
func TestShutdownDrainSparesExitingChild(t *testing.T) {
	child := newStalledChild(t)
	child.exitOnEOF = true
	go func() { _, _ = io.Copy(io.Discard, child.stdinR) }()
	peer := moduleproto.NewPeer(child.codec, moduleproto.RoleHost)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sl := &moduleSlot{name: "m", sup: sup, child: child, peer: peer}

	// The drain call gets no answer, so it waits out its own timeout
	// before stdin closes. The budget runs from the drain call, so that
	// timeout must leave the child time to exit at EOF.
	sup.drainOneForShutdown(sl, 2*time.Second)
	select {
	case <-child.killed:
		t.Fatal("a child that exited at EOF inside its budget was killed")
	default:
	}
	// The shutdown drain leaves the peer open: the child's in-flight
	// reverse calls still reach the queue and the DB.
	if peerClosed(t, peer) {
		t.Fatal("the shutdown drain closed the peer")
	}
}

// TestShutdownDrainKillsChildIgnoringEOF covers the last step: a child
// that reads its stdin but neither answers module.drain nor exits at EOF
// is killed when the budget runs out.
func TestShutdownDrainKillsChildIgnoringEOF(t *testing.T) {
	child := newStalledChild(t)
	go func() { _, _ = io.Copy(io.Discard, child.stdinR) }()
	peer := moduleproto.NewPeer(child.codec, moduleproto.RoleHost)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sl := &moduleSlot{name: "m", sup: sup, child: child, peer: peer}

	done := make(chan struct{})
	go func() {
		sup.drainOneForShutdown(sl, 200*time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the shutdown drain waited past its budget on a child ignoring EOF")
	}
	select {
	case <-child.killed:
	default:
		t.Fatal("the drain returned without killing a child that ignored EOF")
	}
}

// The runtime drain (disable, upgrade) had the same order, inside the
// slot's supervise loop: a child that stopped reading stdin hung the
// loop on the module.drain write, so the module never left Draining.
func TestDisableDrainKillsStalledChild(t *testing.T) {
	child := newStalledChild(t)
	peer := moduleproto.NewPeer(child.codec, moduleproto.RoleHost)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sup.cfg.DrainPerModule = 100 * time.Millisecond
	sl := &moduleSlot{name: "m", sup: sup, child: child, peer: peer, state: StateReady}

	done := make(chan struct{})
	go func() {
		sl.transitionDrain(StateDrainingDisable, 1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the disable drain hung on a child that stopped reading stdin")
	}
	select {
	case <-child.killed:
	default:
		t.Fatal("the disable drain returned without killing the stalled child")
	}
	if sl.state != StateInstalledDisabled || sl.child != nil {
		t.Fatalf("state = %v, child = %v, want InstalledDisabled and no child", sl.state, sl.child)
	}
	if !peerClosed(t, peer) {
		t.Fatal("the disable drain left the killed child's peer open")
	}
}

// TestDisableDrainClosesPeer covers the polite path: the child reads,
// exits at EOF, and the disable drain closes its peer, which cancels the
// child's in-flight reverse calls.
func TestDisableDrainClosesPeer(t *testing.T) {
	child := newStalledChild(t)
	child.exitOnEOF = true
	go func() { _, _ = io.Copy(io.Discard, child.stdinR) }()
	peer := moduleproto.NewPeer(child.codec, moduleproto.RoleHost)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	sup := &ProcessModuleSupervisor{logf: t.Logf}
	sup.cfg.DrainPerModule = 2 * time.Second
	sl := &moduleSlot{name: "m", sup: sup, child: child, peer: peer, state: StateReady}

	sl.transitionDrain(StateDrainingDisable, 1)
	select {
	case <-child.killed:
		t.Fatal("a child that exited at EOF was killed")
	default:
	}
	if !peerClosed(t, peer) {
		t.Fatal("the disable drain left the peer open")
	}
}

// peerClosed reports whether a call on peer fails as closed. An open
// peer over a reading child gets no answer and times out instead.
func peerClosed(t *testing.T, peer *moduleproto.Peer) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := peer.Call(ctx, "probe", nil)
	return errors.Is(err, moduleproto.ErrClosed)
}
