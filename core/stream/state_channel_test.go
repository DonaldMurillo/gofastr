package stream

import (
	"bytes"
	"encoding/json"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// bannerState is the snapshot payload of the test source: the Field
// Assist phone banner.
type bannerState struct {
	Banner string `json:"banner"`
}

// bannerEvent is the event payload: an action plus a field only the
// admin role may see.
type bannerEvent struct {
	Action string `json:"action"`
	Secret string `json:"secret,omitempty"`
}

// bannerSource is a SnapshotSource over one banner. SnapshotFor
// returns the state and its version from one read under the mutex, and
// counts calls so tests can pin the one-immutable-read contract.
type bannerSource struct {
	mu         sync.Mutex
	state      bannerState
	seq        uint64
	snapCalls  int
	filterHits map[string]int
	hideFrom   string // role whose events come back ok=false
}

func newBannerSource() *bannerSource {
	return &bannerSource{filterHits: map[string]int{}}
}

func (s *bannerSource) SnapshotFor(role string) (bannerState, uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapCalls++
	return s.state, s.seq // one read: payload and sequence together
}

func (s *bannerSource) FilterEvent(role string, ev bannerEvent) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.filterHits[role]++
	if role == s.hideFrom {
		return nil, false
	}
	if role == "guest" && ev.Secret != "" {
		// Role filtering: strip the admin-only field before it can be
		// serialized.
		return bannerEvent{Action: ev.Action}, true
	}
	return ev, true
}

func (s *bannerSource) set(banner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = bannerState{Banner: banner}
	s.seq++
}

// newChannelConn returns a test connection whose send buffer is NOT
// drained (no writePump), so tests can assert the exact wire bytes the
// channel queued, in order.
func newChannelConn(buf int) *WebSocketConn {
	return &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(nil), w: &bytes.Buffer{}},
		sendBuffer: make(chan []byte, buf),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}
}

// recvRaw reads the next queued wire message.
func recvRaw(t *testing.T, conn *WebSocketConn) []byte {
	t.Helper()
	select {
	case data := <-conn.sendBuffer:
		return data
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a wire message")
		return nil
	}
}

// recvEnvelope reads and decodes the next wire message.
func recvEnvelope(t *testing.T, conn *WebSocketConn) SequencedEnvelope[json.RawMessage] {
	t.Helper()
	var env SequencedEnvelope[json.RawMessage]
	if err := json.Unmarshal(recvRaw(t, conn), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}

func startChannel(t *testing.T, src *bannerSource) *StateChannel[string, bannerState, bannerEvent] {
	t.Helper()
	c := NewStateChannel[string, bannerState, bannerEvent](src)
	go c.Run()
	// Stop inside the bubble and sleep past CloseTimeout: Stop's
	// detached closers sit in the 1s peer-close wait, and the bubble
	// fails if any goroutine is still blocked when the test returns.
	t.Cleanup(func() {
		c.Stop()
		time.Sleep(2 * time.Second)
	})
	return c
}

func TestStateChannelConnectSendsSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		src.set("HOLD STEADY")
		c := startChannel(t, src)

		conn := newChannelConn(4)
		c.Connect("admin", conn)

		env := recvEnvelope(t, conn)
		if env.Type != SnapshotEnvelopeType {
			t.Fatalf("snapshot Type = %q, want %q", env.Type, SnapshotEnvelopeType)
		}
		if env.Sequence != 1 {
			t.Fatalf("snapshot Sequence = %d, want 1 (source version)", env.Sequence)
		}
		var st bannerState
		if err := json.Unmarshal(env.Payload, &st); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if st.Banner != "HOLD STEADY" {
			t.Fatalf("snapshot banner = %q, want HOLD STEADY", st.Banner)
		}
		if got := c.Count(); got != 1 {
			t.Fatalf("Count after connect = %d, want 1", got)
		}
	})
}

// The snapshot payload and its sequence must come from ONE read of the
// source: state mutated after Connect returns cannot leak into the
// envelope, and SnapshotFor runs exactly once per connect.
func TestStateChannelSnapshotSingleRead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		src.set("HOLD STEADY")
		c := startChannel(t, src)

		conn := newChannelConn(4)
		c.Connect("admin", conn)

		// Mutate after hydration is queued; the envelope must still
		// carry the state from the read that produced sequence 1.
		src.set("CLEARED")

		env := recvEnvelope(t, conn)
		var st bannerState
		if err := json.Unmarshal(env.Payload, &st); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if st.Banner != "HOLD STEADY" {
			t.Fatalf("snapshot banner = %q, want HOLD STEADY (the state at the read)", st.Banner)
		}
		if env.Sequence != 1 {
			t.Fatalf("snapshot Sequence = %d, want 1", env.Sequence)
		}
		if src.snapCalls != 1 {
			t.Fatalf("SnapshotFor called %d times for one connect, want 1", src.snapCalls)
		}
	})
}

func TestStateChannelEventSequencesIncrease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)

		conn := newChannelConn(8)
		c.Connect("admin", conn)
		recvEnvelope(t, conn) // snapshot

		for i, action := range []string{"show", "clear", "show"} {
			c.Publish("banner", bannerEvent{Action: action})
			env := recvEnvelope(t, conn)
			if env.Sequence != uint64(i+1) {
				t.Fatalf("event %d Sequence = %d, want %d", i, env.Sequence, i+1)
			}
			if env.Type != "banner" {
				t.Fatalf("event Type = %q, want banner", env.Type)
			}
		}
	})
}

// The Field Assist ordering guard, server side: the channel's event
// counter must reconcile to every snapshot sequence it sends, so an
// event published after a connection hydrated at the source's version
// 41 carries 42, not a fresh-from-zero counter the client would reject
// as stale.
func TestStateChannelSnapshotReconcilesSequence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		// The application has been mutating for a while: its version
		// counter sits at 41, far ahead of the channel's own count.
		for range 41 {
			src.set("HOLD STEADY")
		}
		c := startChannel(t, src)

		conn := newChannelConn(4)
		c.Connect("admin", conn)
		if env := recvEnvelope(t, conn); env.Sequence != 41 {
			t.Fatalf("snapshot Sequence = %d, want 41", env.Sequence)
		}

		src.set("") // the clear mutation, source version 42
		c.Publish("cleared", bannerEvent{Action: "clear"})

		env := recvEnvelope(t, conn)
		if env.Sequence != 42 {
			t.Fatalf("event after snapshot-41 Sequence = %d, want 42 (reconciled)", env.Sequence)
		}
	})
}
func TestStateChannelWireOrderUnderConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)

		conn := newChannelConn(64)
		c.Connect("admin", conn)
		recvEnvelope(t, conn) // snapshot

		// 40 publishes, 4 concurrent publishers: more than one
		// publisher can enqueue, few enough that the 64-slot job queue
		// can hold the whole burst, so nothing is dropped and every
		// sequence is observable.
		for range 4 {
			go func() {
				for range 10 {
					c.Publish("banner", bannerEvent{Action: "tick"})
				}
			}()
		}
		// synctest.Wait returns once every goroutine is blocked, which
		// for the loop means the job queue is fully drained.
		synctest.Wait()

		var last uint64
		for i := range 40 {
			env := recvEnvelope(t, conn)
			if env.Sequence <= last {
				t.Fatalf("event %d: sequence %d after %d", i, env.Sequence, last)
			}
			last = env.Sequence
		}
	})
}

// Excluded role fields never cross the transport: the guest payload is
// filtered before serialization, so the secret is absent from the raw
// bytes, while the admin role still receives it.
func TestStateChannelRoleFilterStripsBeforeWire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)

		admin := newChannelConn(4)
		c.Connect("admin", admin)
		guest := newChannelConn(4)
		c.Connect("guest", guest)
		recvEnvelope(t, admin) // snapshot
		recvEnvelope(t, guest) // snapshot

		c.Publish("banner", bannerEvent{Action: "rotate", Secret: "sk-live-7f3a"})

		guestRaw := recvRaw(t, guest)
		if bytes.Contains(guestRaw, []byte("sk-live-7f3a")) {
			t.Fatalf("guest wire bytes carry the filtered secret: %s", guestRaw)
		}
		if !bytes.Contains(guestRaw, []byte(`"action":"rotate"`)) {
			t.Fatalf("guest wire bytes lost the permitted action: %s", guestRaw)
		}
		if adminRaw := recvRaw(t, admin); !bytes.Contains(adminRaw, []byte("sk-live-7f3a")) {
			t.Fatalf("admin wire bytes lost the permitted secret: %s", adminRaw)
		}
	})
}

// FilterEvent returning ok=false sends nothing to that role: no
// envelope, not an empty one.
func TestStateChannelFilterHidesRoleEntirely(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		src.hideFrom = "guest"
		c := startChannel(t, src)

		guest := newChannelConn(4)
		c.Connect("guest", guest)
		admin := newChannelConn(4)
		c.Connect("admin", admin)
		recvEnvelope(t, guest) // snapshot
		recvEnvelope(t, admin) // snapshot

		c.Publish("banner", bannerEvent{Action: "rotate"})

		recvEnvelope(t, admin)
		select {
		case data := <-guest.sendBuffer:
			t.Fatalf("guest received %q despite ok=false", data)
		default:
		}
	})
}

// FilterEvent runs once per distinct role, not once per connection:
// three admin connections, one publish, one filter call, three
// identical envelopes.
func TestStateChannelFiltersOncePerRole(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)

		conns := make([]*WebSocketConn, 3)
		for i := range conns {
			conns[i] = newChannelConn(4)
			c.Connect("admin", conns[i])
			recvEnvelope(t, conns[i])
		}

		c.Publish("banner", bannerEvent{Action: "rotate"})
		for _, conn := range conns {
			recvEnvelope(t, conn)
		}

		if got := src.filterHits["admin"]; got != 1 {
			t.Fatalf("FilterEvent called %d times for one role, want 1", got)
		}
	})
}

// A connection that never drains its buffer cannot block the loop or
// starve a healthy connection: events are dropped for the slow one
// only.
func TestStateChannelSlowConnDoesNotBlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)

		slow := newChannelConn(1) // fills after one message, never drained
		healthy := newChannelConn(64)
		c.Connect("admin", slow)
		c.Connect("admin", healthy)
		recvRaw(t, slow) // snapshot fills the buffer exactly
		recvEnvelope(t, healthy)

		for range 16 {
			c.Publish("banner", bannerEvent{Action: "tick"})
		}
		synctest.Wait()

		var last uint64
		for range 16 {
			env := recvEnvelope(t, healthy)
			if env.Sequence <= last {
				t.Fatalf("healthy conn saw sequence %d after %d", env.Sequence, last)
			}
			last = env.Sequence
		}
	})
}

func TestStateChannelUnregisterOnConnClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)

		conn := newChannelConn(4)
		c.Connect("admin", conn)
		recvEnvelope(t, conn)

		conn.Close()
		synctest.Wait()

		if got := c.Count(); got != 0 {
			t.Fatalf("Count after conn close = %d, want 0", got)
		}
	})
}

func TestStateChannelStopClosesConns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := NewStateChannel[string, bannerState, bannerEvent](src)
		go c.Run()

		conn := newChannelConn(4)
		c.Connect("admin", conn)

		c.Stop()
		synctest.Wait()

		select {
		case <-conn.Closed():
		default:
			t.Fatal("connection not closed after channel stop")
		}
		// Stop's detached closers sit in the 1s peer-close wait; sleep
		// past it (fake time) so they exit before the bubble does.
		time.Sleep(2 * time.Second)
	})
}

// Connect after Stop must not block, and closes the connection it was
// handed: one that can never hydrate is useless.
func TestStateChannelConnectAfterStopCloses(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := NewStateChannel[string, bannerState, bannerEvent](src)
		go c.Run()
		c.Stop()
		synctest.Wait()

		conn := newChannelConn(4)
		c.Connect("admin", conn)

		select {
		case <-conn.Closed():
		case <-time.After(2 * time.Second):
			t.Fatal("Connect after Stop did not close the connection")
		}
		if got := c.Count(); got != 0 {
			t.Fatalf("Count after stop+connect = %d, want 0", got)
		}
		// The detached closer sits in the 1s peer-close wait; sleep
		// past it (fake time) so it exits before the bubble does.
		time.Sleep(2 * time.Second)
	})
}

// A connection whose buffer is too full to accept its hydration
// snapshot is closed (it reconnects), not left blind.
func TestStateChannelFullBufferClosesConn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)

		conn := newChannelConn(1)
		conn.sendBuffer <- []byte("occupied")

		c.Connect("admin", conn)
		synctest.Wait()

		select {
		case <-conn.Closed():
		case <-time.After(2 * time.Second):
			t.Fatal("conn with unqueueable snapshot was not closed")
		}
	})
}

func TestStateChannelPublishAfterStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := NewStateChannel[string, bannerState, bannerEvent](src)
		go c.Run()
		c.Stop()
		synctest.Wait()

		c.Publish("banner", bannerEvent{Action: "tick"}) // must not block or panic
	})
}

// TestStateChannelOverflowClosesWhenAsked: with CloseOnOverflow the
// connection that cannot take an event is closed instead of losing it,
// so it reconnects and re-hydrates; the healthy one is untouched.
func TestStateChannelOverflowClosesWhenAsked(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newBannerSource()
		c := startChannel(t, src)
		c.CloseOnOverflow(true)

		slow := newChannelConn(1) // fills after one message, never drained
		healthy := newChannelConn(64)
		c.Connect("admin", slow)
		c.Connect("admin", healthy)
		recvRaw(t, slow) // snapshot fills the buffer exactly
		recvEnvelope(t, healthy)

		c.Publish("banner", bannerEvent{Action: "tick"}) // queued: buffer has one slot
		c.Publish("banner", bannerEvent{Action: "tick"}) // overflow
		synctest.Wait()

		select {
		case <-slow.Closed():
		default:
			t.Fatal("overflowed connection was left open with an event dropped for it")
		}
		select {
		case <-healthy.Closed():
			t.Fatal("healthy connection was closed")
		default:
		}
		for range 2 {
			recvEnvelope(t, healthy)
		}
	})
}

// TestCloseWithStatusWritesCodeAndReason: the close frame carries the
// code and a reason bounded to 123 bytes with control bytes stripped.
func TestCloseWithStatusWritesCodeAndReason(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		out := &bytes.Buffer{}
		conn := &WebSocketConn{
			conn:       &nopConn{r: bytes.NewReader(nil), w: out},
			sendBuffer: make(chan []byte, 1),
			closed:     make(chan struct{}),
			peerClosed: make(chan struct{}),
			config:     WSConfig{ReadLimit: 1 << 20},
		}
		long := strings.Repeat("r", 200)
		if err := conn.CloseWithStatus(4409, "room\x00 full\x7f"+long); err != nil {
			t.Fatalf("CloseWithStatus: %v", err)
		}
		reason := "room full" + long
		reason = reason[:123]
		want := append([]byte{0x88, byte(2 + len(reason)), 0x11, 0x39}, reason...)
		if !bytes.Equal(out.Bytes(), want) {
			t.Fatalf("close frame = %x, want %x", out.Bytes(), want)
		}
	})
}

// blockedConn's Write blocks until release is closed: a peer whose TCP
// send buffer is full, so the writePump sits inside the kernel write
// holding c.mu for up to WriteTimeout.
type blockedConn struct {
	release chan struct{}
	mu      sync.Mutex
	writes  int
}

func (b *blockedConn) Read(p []byte) (int, error) { <-b.release; return 0, io.EOF }
func (b *blockedConn) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.writes++
	b.mu.Unlock()
	<-b.release
	return len(p), nil
}
func (b *blockedConn) Close() error { return nil }

// TestStateChannelOverflowClosesOncePerConn: with CloseOnOverflow, a
// stalled socket costs one closing goroutine however many events pile
// up behind it. c.closed is closed only once Close acquires c.mu, which
// the stalled writePump holds for up to WriteTimeout, so the Closed()
// guard alone let every further event spawn another blocked Close.
func TestStateChannelOverflowClosesOncePerConn(t *testing.T) {
	bc := &blockedConn{release: make(chan struct{})}
	conn := &WebSocketConn{
		conn:       bc,
		sendBuffer: make(chan []byte, 1),
		closed:     make(chan struct{}),
		peerClosed: make(chan struct{}),
		readMsgs:   make(chan []byte, 8),
		readDone:   make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20, WriteTimeout: 10 * time.Second, CloseTimeout: time.Second},
	}
	go conn.writePump()
	src := newBannerSource()
	c := NewStateChannel[string, bannerState, bannerEvent](src)
	c.CloseOnOverflow(true)
	go c.Run()
	t.Cleanup(func() { close(bc.release); c.Stop() })

	c.Connect("admin", conn)
	deadline := time.Now().Add(5 * time.Second)
	for {
		bc.mu.Lock()
		stalled := bc.writes > 0
		bc.mu.Unlock()
		if stalled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the write never stalled")
		}
		time.Sleep(time.Millisecond)
	}

	before := runtime.NumGoroutine()
	const events = 500
	for range events {
		c.Publish("banner", bannerEvent{Action: "tick"})
		time.Sleep(50 * time.Microsecond)
	}
	time.Sleep(200 * time.Millisecond)
	if peak := runtime.NumGoroutine(); peak-before > 4 {
		t.Fatalf("%d undeliverable events on one stalled socket grew goroutines by %d, want one closer", events, peak-before)
	}
}

// TestCloseWithStatusRefusesReservedCodes: a code RFC 6455 reserves or
// that is out of range never reaches the wire; the frame carries 1002
// and the caller learns it asked for something illegal.
func TestCloseWithStatusRefusesReservedCodes(t *testing.T) {
	for _, code := range []uint16{0, 999, 1004, 1005, 1006, 1015, 5000} {
		synctest.Test(t, func(t *testing.T) {
			out := &bytes.Buffer{}
			conn := &WebSocketConn{
				conn:       &nopConn{r: bytes.NewReader(nil), w: out},
				sendBuffer: make(chan []byte, 1),
				closed:     make(chan struct{}),
				peerClosed: make(chan struct{}),
				config:     WSConfig{ReadLimit: 1 << 20},
			}
			err := conn.CloseWithStatus(code, "x")
			if err == nil {
				t.Fatalf("code %d: want an error", code)
			}
			if got := out.Bytes(); len(got) < 4 || got[2] != 0x03 || got[3] != 0xEA {
				t.Fatalf("code %d: frame %x, want close code 1002", code, got)
			}
		})
	}
}
