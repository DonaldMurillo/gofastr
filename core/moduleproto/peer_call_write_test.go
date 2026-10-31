package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"
)

// A Call wrote its request frame before it looked at ctx, and the codec
// write has no deadline, so a counterparty that stopped reading held the
// caller past its deadline for good. A process module's proxy call is an
// HTTP handler, so each one parked a request goroutine.
func TestCallReturnsWhileWriteBlocked(t *testing.T) {
	peer, _ := blockedWritePeer(t, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		_, err := peer.Call(ctx, "echo", nil)
		errc <- err
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a Call whose write blocked ignored its deadline")
	}
}

// TestAbandonedWriteKeepsInflightSlot checks a Call that gave up on a
// blocked write keeps its inflight slot until the write returns, so
// abandoned writes stay bounded by the inflight cap.
func TestAbandonedWriteKeepsInflightSlot(t *testing.T) {
	peer, outR := blockedWritePeer(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := peer.Call(ctx, "echo", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("first call err = %v, want DeadlineExceeded", err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel2()
	if _, err := peer.Call(ctx2, "echo", nil); !errors.Is(err, ErrInflightCap) {
		t.Fatalf("second call err = %v, want ErrInflightCap while the first write is pending", err)
	}

	// The counterparty reads again: the write returns and frees the slot.
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, err := peer.Call(ctx, "echo", nil)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			return // admitted, written, and unanswered
		}
		if !errors.Is(err, ErrInflightCap) {
			t.Fatalf("call err = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the slot stayed held after the abandoned write returned")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestWrittenCallsFreeTheirSlot checks a Call whose write returned frees
// its slot when it gives up on the answer, so a counterparty that reads
// but never answers does not use the cap up.
func TestWrittenCallsFreeTheirSlot(t *testing.T) {
	peer, outR := blockedWritePeer(t, 1)
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	for i := range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := peer.Call(ctx, "echo", nil)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("call %d err = %v, want DeadlineExceeded", i, err)
		}
	}
}

// TestExpiredCallSendsNothing checks a Call whose ctx already ended
// writes no frame: the counterparty would run a request nobody awaits.
func TestExpiredCallSendsNothing(t *testing.T) {
	peer, outR := blockedWritePeer(t, 4)
	got := make(chan int, 1)
	go func() {
		buf := make([]byte, 64)
		n, _ := outR.Read(buf)
		got <- n
	}()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := peer.Call(expired, "echo", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want Canceled", err)
	}
	select {
	case n := <-got:
		t.Fatalf("an expired Call wrote %d bytes", n)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestAnsweredCallFreesSlotBeforeReturn checks a Call answered while its
// Write had not yet returned still frees its slot before it returns. The
// first async draft returned at the answer and freed the slot after, so a
// caller at the cap saw ErrInflightCap on its next Call
// (TestGate_TransportLatencySaturated lost 30 of 12800 calls).
func TestAnsweredCallFreesSlotBeforeReturn(t *testing.T) {
	inR, inW := io.Pipe()
	t.Cleanup(func() { _ = inW.Close() })
	codec, err := NewCodec(inR, answerFirstWriter{in: inW}, 0)
	if err != nil {
		t.Fatal(err)
	}
	peer := NewPeer(codec, RoleHost, WithMaxInflight(1))
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	for i := range 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err := peer.Call(ctx, "echo", nil)
		cancel()
		if err != nil {
			t.Fatalf("call %d err = %v", i, err)
		}
	}
}

// answerFirstWriter answers each request frame before its Write returns.
type answerFirstWriter struct{ in *io.PipeWriter }

func (w answerFirstWriter) Write(b []byte) (int, error) {
	var req struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(b, &req); err != nil {
		return 0, err
	}
	go func() { _, _ = fmt.Fprintf(w.in, `{"jsonrpc":"2.0","id":%d,"result":1}`+"\n", req.ID) }()
	time.Sleep(50 * time.Millisecond) // the answer lands while Write is still running
	return len(b), nil
}

// TestCloseEndsBlockedCall checks Close ends a Call with no deadline
// whose write blocked.
func TestCloseEndsBlockedCall(t *testing.T) {
	peer, _ := blockedWritePeer(t, 4)
	errc := make(chan error, 1)
	go func() {
		_, err := peer.Call(context.Background(), "echo", nil)
		errc <- err
	}()
	time.Sleep(20 * time.Millisecond) // the Call reaches its write
	_ = peer.Close()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v, want ErrClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close left a Call blocked on its write")
	}
}

// blockedWritePeer returns a started Peer whose writes block until the
// returned reader is read, and whose counterparty never answers.
func blockedWritePeer(t *testing.T, inflight int) (*Peer, *io.PipeReader) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	t.Cleanup(func() {
		_ = outR.Close()
		_ = inW.Close()
	})
	codec, err := NewCodec(inR, outW, 0)
	if err != nil {
		t.Fatal(err)
	}
	peer := NewPeer(codec, RoleHost, WithMaxInflight(inflight))
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	return peer, outR
}
