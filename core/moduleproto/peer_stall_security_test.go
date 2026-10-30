package moduleproto

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
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
			// One response blocks inside the write nobody reads, serveCap
			// more wait behind it, and the next one faults the peer.
			if n := served.Load(); n != serveCap+2 {
				t.Fatalf("%d requests served before the stall fault, want %d", n, serveCap+2)
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
		waitParked(t, peer, min(i, serveCap))
	}
	t.Fatalf("SECURITY: [moduleproto-stall] 20 paced requests whose responses nobody read left %d goroutines parked on the write and never faulted the peer", served.Load())
}

// TestReadingCounterpartyNeverStalls pins the other side: a counterparty
// that keeps the cap's worth of calls open and reads every answer can
// send far more requests than the cap. It used to send the next request
// once the handler ran, before reading the answer, which holds one call
// over the cap and faulted the peer on a loaded machine.
func TestReadingCounterpartyNeverStalls(t *testing.T) {
	const serveCap, total = 2, 50
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
	peer := NewPeer(codec, RoleHost,
		WithMaxServeInflight(serveCap),
		WithHandler("echo", func(ctx context.Context, params json.RawMessage) (any, error) {
			return "ok", nil
		}),
	)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	send := func(id int) {
		line := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"echo"}`+"\n", id)
		if _, err := inW.Write([]byte(line)); err != nil {
			t.Error(err)
		}
	}
	// The counterparty keeps serveCap calls open and sends the next one
	// only after it has read an answer, the way a conforming caller at
	// the cap does.
	answers := make(chan struct{}, total)
	go func() {
		sc := bufio.NewScanner(outR)
		for sc.Scan() {
			answers <- struct{}{}
		}
		// The pipe closes at cleanup; any other read error leaves an
		// answer unread, which the wait below reports.
		_ = sc.Err()
	}()
	next := 1
	for ; next <= serveCap; next++ {
		send(next)
	}
	for range total {
		select {
		case <-answers:
		case <-peer.FatalDone():
			t.Fatalf("a reading counterparty faulted the peer: %v", peer.FatalError())
		case <-time.After(5 * time.Second):
			t.Fatal("request not answered")
		}
		if next <= total {
			send(next)
			next++
		}
	}
	if err := peer.FatalError(); err != nil {
		t.Fatalf("a reading counterparty faulted the peer: %v", err)
	}
}

// A response counted as unwritten until its goroutine retook the lock
// after the write returned, but the counterparty can read it, free its
// slot and send the next call before then. At the cap that lag read as
// one call too many, and a conforming counterparty faulted the peer:
// a process module at its 32-call ceiling stopped answering, and every
// host call waited out its deadline (CI, PR #624).
func TestCallersAtTheCapNeverStall(t *testing.T) {
	for _, cap := range []int{1, 4} {
		t.Run(fmt.Sprint("cap", cap), func(t *testing.T) {
			hostR, childW := io.Pipe()
			childR, hostW := io.Pipe()
			hostCodec, err := NewCodec(hostR, hostW, 0)
			if err != nil {
				t.Fatal(err)
			}
			childCodec, err := NewCodec(childR, childW, 0)
			if err != nil {
				t.Fatal(err)
			}
			host := NewPeer(hostCodec, RoleHost, WithMaxInflight(cap))
			child := NewPeer(childCodec, RoleChild, WithMaxServeInflight(cap),
				WithHandler("echo", func(context.Context, json.RawMessage) (any, error) { return "ok", nil }))
			host.Start()
			child.Start()
			t.Cleanup(func() {
				_ = host.Close()
				_ = child.Close()
				_ = hostW.Close()
				_ = childW.Close()
			})
			done := make(chan error, cap)
			for range cap {
				go func() {
					for range 2000 {
						ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
						_, err := host.Call(ctx, "echo", nil)
						cancel()
						if err != nil {
							done <- err
							return
						}
					}
					done <- nil
				}()
			}
			for range cap {
				if err := <-done; err != nil {
					t.Fatalf("a call at the cap failed: %v (child fault: %v)", err, child.FatalError())
				}
			}
			if err := child.FatalError(); err != nil {
				t.Fatalf("callers within the cap faulted the serving peer: %v", err)
			}
		})
	}
}

// A served response the codec cannot write faults the peer, so the
// caller's open call ends instead of waiting on an answer that never
// comes.
func TestUnwritableResponseFaultsPeer(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	t.Cleanup(func() {
		_ = outR.Close()
		_ = inW.Close()
	})
	go func() { _, _ = io.Copy(io.Discard, outR) }()
	codec, err := NewCodec(inR, outW, 256)
	if err != nil {
		t.Fatal(err)
	}
	peer := NewPeer(codec, RoleChild,
		WithHandler("big", func(context.Context, json.RawMessage) (any, error) {
			return strings.Repeat("x", 1024), nil
		}),
	)
	peer.Start()
	t.Cleanup(func() { _ = peer.Close() })
	if _, err := inW.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"big"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-peer.FatalDone():
		var over *OvercapError
		if !errors.As(peer.FatalError(), &over) {
			t.Fatalf("fatal = %v, want an OvercapError", peer.FatalError())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("an unwritable response left the peer serving")
	}
}

// waitParked waits until want served responses wait behind a blocked
// write, or the peer faulted.
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
