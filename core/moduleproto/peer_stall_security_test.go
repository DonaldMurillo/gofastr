package moduleproto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"
)

// Property (2026-10-27 bug bash, P3): a counterparty that stops reading
// responses cannot grow the serving peer without bound. A served
// request's goroutine released its serve slot when the handler returned
// and then parked on the codec write, so a child sending paced requests
// it never read the answers to got one parked goroutine per request.
// Process modules are the isolation boundary for untrusted code, so the
// child is the adversary here.
func TestStalledReaderFaultsPeer(t *testing.T) {
	const serveCap = 2
	inR, inW := io.Pipe()   // the counterparty writes requests here
	outR, outW := io.Pipe() // the peer's responses; nobody reads them
	t.Cleanup(func() {
		_ = outR.Close()
		_ = inW.Close()
	})
	codec, err := NewCodec(inR, outW, 0)
	if err != nil {
		t.Fatal(err)
	}
	var served atomic.Int64
	handled := make(chan struct{}, 64)
	peer := NewPeer(codec, RoleHost,
		WithMaxServeInflight(serveCap),
		WithHandler("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
			served.Add(1)
			handled <- struct{}{}
			return "ok", nil
		}),
	)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })

	// Paced: each request waits for the previous handler, so the serve
	// cap itself never trips and only the unwritten responses pile up.
	for i := range 20 {
		select {
		case <-peer.FatalDone():
			if !errors.Is(peer.FatalError(), ErrStalled) {
				t.Fatalf("fatal = %v, want ErrStalled", peer.FatalError())
			}
			if n := served.Load(); n != serveCap+1 {
				t.Fatalf("%d requests served before the stall fault, want %d", n, serveCap+1)
			}
			return
		default:
		}
		line := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"echo"}`+"\n", i+1)
		if _, err := inW.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		select {
		case <-handled:
		case <-peer.FatalDone():
		case <-time.After(2 * time.Second):
			t.Fatal("request not served")
		}
		waitParked(t, peer, min(i+1, serveCap))
	}
	t.Fatalf("SECURITY: [moduleproto-stall] 20 paced requests whose responses nobody read left %d goroutines parked on the write and never faulted the peer", served.Load())
}

// TestReadingCounterpartyNeverStalls pins the other side: a counterparty
// that reads its responses can send far more requests than the cap.
func TestReadingCounterpartyNeverStalls(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	t.Cleanup(func() {
		_ = outR.Close()
		_ = inW.Close()
	})
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	codec, err := NewCodec(inR, outW, 0)
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan struct{}, 64)
	peer := NewPeer(codec, RoleHost,
		WithMaxServeInflight(2),
		WithHandler("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
			handled <- struct{}{}
			return "ok", nil
		}),
	)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	for i := range 50 {
		line := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"echo"}`+"\n", i+1)
		if _, err := inW.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		select {
		case <-handled:
		case <-time.After(2 * time.Second):
			t.Fatal("request not served")
		}
	}
	time.Sleep(20 * time.Millisecond)
	if err := peer.FatalError(); err != nil {
		t.Fatalf("a reading counterparty faulted the peer: %v", err)
	}
}

// waitParked waits until want served responses wait on their write, or
// the peer faulted.
func waitParked(t *testing.T, p *Peer, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		n := p.respPending
		p.mu.Unlock()
		select {
		case <-p.FatalDone():
			return
		default:
		}
		if n >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%d responses never reached their write", want)
}

// TestFaultedPeerServesNothing checks a request read after a fault runs
// no handler. An unsolicited response faults the peer and the read loop
// keeps reading; a host handler run then would act for a child the
// supervisor is tearing down.
func TestFaultedPeerServesNothing(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	t.Cleanup(func() {
		_ = outR.Close()
		_ = inW.Close()
	})
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	codec, err := NewCodec(inR, outW, 0)
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan struct{}, 1)
	peer := NewPeer(codec, RoleHost,
		WithHandler("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
			handled <- struct{}{}
			return "ok", nil
		}),
	)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	if _, err := inW.Write([]byte(`{"jsonrpc":"2.0","id":99,"result":1}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-peer.FatalDone():
	case <-time.After(2 * time.Second):
		t.Fatal("an unsolicited response did not fault the peer")
	}
	go func() { _, _ = inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"echo"}` + "\n")) }()
	select {
	case <-handled:
		t.Fatal("a faulted peer ran a handler for a request read after the fault")
	case <-time.After(300 * time.Millisecond):
	}
}
