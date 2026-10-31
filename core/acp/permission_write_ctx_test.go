package acp

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

// 2026-10-27 P7: RequestPermission's frame write ignored its context. It
// waited for the write slot on close alone and then wrote straight into
// the output, so a client that stopped reading stdout left the call, and
// the prompt turn waiting on it, stuck after the turn was cancelled.

// TestPermissionCancelsBlockedWrite has the permission frame itself stuck
// in a write nobody reads.
func TestPermissionCancelsBlockedWrite(t *testing.T) {
	r, w := io.Pipe() // nothing reads r: any write blocks
	defer r.Close()
	client := &Client{conn: newConn(w), sessionID: "s1"}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := client.RequestPermission(ctx, ToolCallUpdate{ToolCallID: "t1"}, nil)
		errs <- err
	}()
	select {
	case err := <-errs:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the context's deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled permission request stayed blocked in its frame write")
	}
}

// TestPermissionCancelsQueuedWrite queues the permission frame behind an
// update that holds the write slot.
func TestPermissionCancelsQueuedWrite(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	client := &Client{conn: newConn(w), sessionID: "s1"}
	go func() { _ = client.Update(AgentMessageChunk("m1", "stuck")) }()
	time.Sleep(20 * time.Millisecond) // the update takes the slot
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	errs := make(chan error, 1)
	go func() {
		_, err := client.RequestPermission(ctx, ToolCallUpdate{ToolCallID: "t1"}, nil)
		errs <- err
	}()
	select {
	case err := <-errs:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want the context's deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled permission request stayed queued for the write slot")
	}
}

// TestAbandonedWriteFreesSlot checks the write a cancel walks away from
// still releases the slot once the output drains, and that the frame it
// carried went out whole, so the next frame starts on a line of its own.
func TestAbandonedWriteFreesSlot(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	client := &Client{conn: newConn(w), sessionID: "s1"}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.RequestPermission(ctx, ToolCallUpdate{ToolCallID: "t1"}, nil); err == nil {
		t.Fatal("permission request on an unread output succeeded")
	}
	read := make(chan []byte, 1)
	go func() {
		buf, _ := io.ReadAll(r)
		read <- buf
	}()
	errs := make(chan error, 1)
	go func() { errs <- client.Update(AgentMessageChunk("m1", "after")) }()
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("update after the abandoned write: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned write never released the write slot")
	}
	_ = w.Close()
	lines := splitLines(<-read)
	if len(lines) != 2 {
		t.Fatalf("output has %d lines, want the permission frame and the update: %q", len(lines), lines)
	}
}

// TestCloseFreesBlockedPermission closes the connection while the
// permission frame is stuck in its write.
func TestCloseFreesBlockedPermission(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	c := newConn(w)
	client := &Client{conn: c, sessionID: "s1"}
	errs := make(chan error, 1)
	go func() {
		_, err := client.RequestPermission(context.Background(), ToolCallUpdate{ToolCallID: "t1"}, nil)
		errs <- err
	}()
	time.Sleep(20 * time.Millisecond) // the frame write takes the slot
	c.close()
	select {
	case err := <-errs:
		if !errors.Is(err, errConnClosed) {
			t.Fatalf("err = %v, want errConnClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a permission request stuck in its frame write outlived the connection")
	}
}

// TestWriterPanicBreaksStream has the output writer panic under a
// cancellable permission request. The write runs on its own goroutine
// there, so the panic must come back as an error, and the stream, with
// an unknown part of a frame written, refuses every later frame.
func TestWriterPanicBreaksStream(t *testing.T) {
	client := &Client{conn: newConn(panicWriter{}), sessionID: "s1"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := client.RequestPermission(ctx, ToolCallUpdate{ToolCallID: "t1"}, nil); !errors.Is(err, errWriterPanicked) {
		t.Fatalf("err = %v, want errWriterPanicked", err)
	}
	if err := client.Update(AgentMessageChunk("m1", "after")); !errors.Is(err, errWriterPanicked) {
		t.Fatalf("update after the panic: err = %v, want errWriterPanicked", err)
	}
}

// TestCancelledPermissionSendsNothing calls with a context that is
// already done while the write slot is free. select picks at random
// between two ready arms, so the call runs many times: none may send
// its frame.
func TestCancelledPermissionSendsNothing(t *testing.T) {
	var out countingWriter
	client := &Client{conn: newConn(&out), sessionID: "s1"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 64 {
		if _, err := client.RequestPermission(ctx, ToolCallUpdate{ToolCallID: "t1"}, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	}
	if n := out.n.Load(); n != 0 {
		t.Fatalf("a cancelled permission request wrote %d frames", n)
	}
	errs := make(chan error, 1)
	go func() { errs <- client.Update(AgentMessageChunk("m1", "after")) }()
	select {
	case err := <-errs:
		if err != nil || out.n.Load() != 1 {
			t.Fatalf("update after cancelled calls: err = %v, frames = %d", err, out.n.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled permission request kept the write slot")
	}
}

type countingWriter struct{ n atomic.Int64 }

func (w *countingWriter) Write(b []byte) (int, error) {
	w.n.Add(1)
	return len(b), nil
}

type panicWriter struct{}

func (panicWriter) Write([]byte) (int, error) { panic("writer") }

func splitLines(b []byte) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, string(b[start:i]))
			start = i + 1
		}
	}
	return out
}
