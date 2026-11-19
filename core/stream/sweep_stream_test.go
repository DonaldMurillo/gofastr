package stream

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/internal/sseframe"
)

var sweepHostileFrames = []struct{ id, name, data string }{
	{"", "", ""},
	{"7", "tick", "hello"},
	{"1\r\nevent: forged", "ok\ndata: pwned", "a\r\nb\n\nevent: forged\x00c"},
	{"\x00", "x\x00y", "\n\n"},
	{"\r", "name", "line1\nline2\r\nline3"},
}

func TestSweepSSEEncodersAgree(t *testing.T) {
	for _, c := range sweepHostileFrames {
		want := Encode(Event{Type: Custom, Name: c.name, ID: c.id, Data: c.data})

		rec := httptest.NewRecorder()
		if err := (handler.SSE{Event: c.name, Data: c.data, ID: c.id}).WriteBody(rec); err != nil {
			t.Fatal(err)
		}
		if got := rec.Body.String(); got != want {
			t.Errorf("handler.SSE and stream.Encode disagree for %+v:\nhandler %q\nstream  %q", c, got, want)
		}

		rec = httptest.NewRecorder()
		w := NewSSEWriter(rec)
		w.SetID(c.id)
		if sseframe.Field(c.name) == "" {
			err := w.WriteData(c.data)
			want = Encode(Event{Type: Message, ID: c.id, Data: c.data})
			if err != nil {
				t.Fatal(err)
			}
		} else if err := w.WriteEvent(c.name, c.data); err != nil {
			t.Fatal(err)
		}
		if got := rec.Body.String(); got != want {
			t.Errorf("SSEWriter and stream.Encode disagree for %+v:\nwriter %q\nstream %q", c, got, want)
		}
	}
}

func TestSweepBlockModeSendIsBounded(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{AllowClientSlowMode: true, BlockTimeout: 30 * time.Millisecond})
	stuck := &subscriber{ch: make(chan sseEvent, 1), done: make(chan struct{}), slowMode: sseSlowBlock}
	stuck.ch <- sseEvent{Name: "full"}
	b.mu.Lock()
	b.subscribers["stuck"] = stuck
	b.mu.Unlock()

	returned := make(chan struct{})
	go func() {
		b.Publish("e", "d")
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish stayed blocked on a block-mode subscriber that stopped reading past BlockTimeout")
	}
}

type sweepSeat struct{ done chan struct{} }

func (s *sweepSeat) SeatDone() chan struct{} { return s.done }

func TestSweepAdmitSeatPolicies(t *testing.T) {
	if got := seatsFor(0); got != 16 {
		t.Fatalf("a zero cap resolves to %d seats, want 16", got)
	}
	newSeat := func(string) *sweepSeat { return &sweepSeat{done: make(chan struct{})} }

	order := map[string][]*sweepSeat{}
	first, _ := AdmitSeat(order, "p", 2, SeatOverflowRefuse, newSeat)
	AdmitSeat(order, "p", 2, SeatOverflowRefuse, newSeat)
	if _, ok := AdmitSeat(order, "p", 2, SeatOverflowRefuse, newSeat); ok {
		t.Fatal("Refuse admitted a seat past the cap")
	}
	if _, ok := AdmitSeat(order, "q", 2, SeatOverflowRefuse, newSeat); !ok {
		t.Fatal("one principal's full cap refused another principal")
	}

	third, ok := AdmitSeat(order, "p", 2, SeatOverflowEvictOldest, newSeat)
	if !ok || len(order["p"]) != 2 || order["p"][1] != third {
		t.Fatalf("EvictOldest did not seat the new member in place of the oldest: %v", order["p"])
	}
	select {
	case <-first.done:
	default:
		t.Fatal("EvictOldest did not close the evicted member's done channel")
	}
	for _, s := range order["p"] {
		if s == first {
			t.Fatal("the evicted member is still in the FIFO")
		}
	}

	SpliceSeat(order, "q", order["q"][0])
	if _, present := order["q"]; present {
		t.Fatal("SpliceSeat left an empty FIFO under its key")
	}

	unlimited := map[string][]*sweepSeat{}
	for range 40 {
		if _, ok := AdmitSeat(unlimited, "p", -1, SeatOverflowRefuse, newSeat); !ok {
			t.Fatal("a negative cap refused a seat")
		}
	}
}

func TestSweepEvictOldestSeatsTheNewStream(t *testing.T) {
	b := NewSSEBroker(SSEBrokerConfig{
		MaxSeatsPerPrincipal: 1,
		SeatOverflow:         SeatOverflowEvictOldest,
		Principal:            func(r *http.Request) string { return "user-1" },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	open := func(id string) chan struct{} {
		done := make(chan struct{})
		req := httptest.NewRequest(http.MethodGet, "/events?subscriber_id="+id, nil).WithContext(ctx)
		go func() { b.Subscribe(httptest.NewRecorder(), req); close(done) }()
		return done
	}
	firstDone := open("a")
	waitForSubscribers(t, b, 1)
	open("b")
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("EvictOldest did not end the oldest stream")
	}
	waitUntil(t, func() bool {
		b.mu.RLock()
		defer b.mu.RUnlock()
		_, seated := b.subscribers["b"]
		return seated && len(b.subscribers) == 1
	})
}

func TestSweepOwnFanoutEchoIsDropped(t *testing.T) {
	f := fanout.NewInProcess()
	b := NewSSEBroker(SSEBrokerConfig{Topic: "echo", Fanout: f})
	defer b.Close()
	sub := &subscriber{ch: make(chan sseEvent, 8), done: make(chan struct{})}
	b.mu.Lock()
	b.subscribers["x"] = sub
	b.mu.Unlock()

	b.Publish("e", "once")
	time.Sleep(200 * time.Millisecond)
	if got := len(sub.ch); got != 1 {
		t.Fatalf("one Publish reached a local subscriber %d times, want 1 (the replica's own fanout echo is dropped)", got)
	}
}

func TestSweepUpgradeSettlesDefaults(t *testing.T) {
	cfg := upgradeOnce(t, WSConfig{}).config
	want := WSConfig{
		ReadLimit: 1 << 20, SendBuffer: 32, WriteTimeout: 10 * time.Second,
		ReadIdleTimeout: 60 * time.Second, PongTimeout: 10 * time.Second,
		CloseTimeout: time.Second, MaxControlFramesPerSecond: 32,
	}
	got := WSConfig{
		ReadLimit: cfg.ReadLimit, SendBuffer: cfg.SendBuffer, WriteTimeout: cfg.WriteTimeout,
		ReadIdleTimeout: cfg.ReadIdleTimeout, PongTimeout: cfg.PongTimeout,
		CloseTimeout: cfg.CloseTimeout, MaxControlFramesPerSecond: cfg.MaxControlFramesPerSecond,
	}
	if !reflect.DeepEqual(got, want) || !cfg.requireMask {
		t.Fatalf("Upgrade with a zero config settled %+v (requireMask %v), want %+v with requireMask", got, cfg.requireMask, want)
	}
}

func TestSweepOversizeControlFrameFailsWith1002(t *testing.T) {
	var frame bytes.Buffer
	frame.WriteByte(0x80 | wsopcodePing)
	frame.WriteByte(126)
	frame.Write([]byte{0x00, 0x7E})
	frame.Write(make([]byte, 126))
	conn := &WebSocketConn{
		conn:       &nopConn{r: bytes.NewReader(frame.Bytes()), w: io.Discard},
		sendBuffer: make(chan wsMessage, 8),
		closed:     make(chan struct{}),
		config:     WSConfig{ReadLimit: 1 << 20},
	}
	if _, err := conn.readFrame(); err == nil {
		t.Fatal("a 126-byte Ping was read without error")
	}
	if own := conn.ownClosePayload.Load(); own == nil || !bytes.Equal(*own, []byte{0x03, 0xEA}) {
		t.Fatalf("a 126-byte Ping recorded close payload %v, want 1002", own)
	}
}

func TestSweepPushOnlyConnSurvivesAStall(t *testing.T) {
	conn, cli := newReadPumpConn(t, WSConfig{
		ReadLimit:       1 << 20,
		ReadIdleTimeout: 40 * time.Millisecond,
		PongTimeout:     40 * time.Millisecond,
		WriteTimeout:    time.Second,
		CloseTimeout:    100 * time.Millisecond,
		requireMask:     true,
	})
	defer cli.Close()
	sendDataFrames(t, cli, 12)
	var answered int32
	stop := make(chan struct{})
	defer close(stop)
	go pongAnsweringPeer(cli, &answered, stop)

	time.Sleep(500 * time.Millisecond)
	select {
	case <-conn.Closed():
		t.Fatal("a push-only connection with a live peer was closed after its unread buffer filled")
	default:
	}
	conn.Close()
}

func TestSweepHubNeverWaitsOnAFullConnection(t *testing.T) {
	stopped := NewHub()
	for range 200 {
		if err := stopped.Broadcast([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}

	hub := NewHub()
	go hub.Run()
	defer hub.Stop()
	full := newTestConn()
	full.sendBuffer = make(chan wsMessage, 1)
	hub.Register(full)
	for range 3 {
		_ = hub.BroadcastWait([]byte("x"))
	}
	time.Sleep(100 * time.Millisecond)
	fresh := newTestConn()
	hub.Register(fresh)
	deadline := time.After(2 * time.Second)
	for {
		_ = hub.Broadcast([]byte("y"))
		select {
		case <-fresh.sendBuffer:
			return
		case <-deadline:
			t.Fatal("a connection with a full send buffer held the hub's broadcast loop")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// WriteEvent always writes an event line, so a name that is empty, or
// empty once cut, gives "event: " where Encode and handler.SSE write
// none. Both dispatch as "message" in a browser; the bytes are pinned.
func TestWriteEventEmptyNameKeepsEventLine(t *testing.T) {
	for _, name := range []string{"", "\r", "\nevent: forged"} {
		rec := httptest.NewRecorder()
		if err := NewSSEWriter(rec).WriteEvent(name, "x"); err != nil {
			t.Fatal(err)
		}
		if got, want := rec.Body.String(), "event: \ndata: x\n\n"; got != want {
			t.Errorf("WriteEvent(%q) wrote %q, want %q", name, got, want)
		}
		if got, want := Encode(Event{Type: Custom, Name: name, Data: "x"}), "data: x\n\n"; got != want {
			t.Errorf("Encode(Custom %q) = %q, want %q", name, got, want)
		}
		rec = httptest.NewRecorder()
		if err := (handler.SSE{Event: name, Data: "x"}).WriteBody(rec); err != nil {
			t.Fatal(err)
		}
		if got, want := rec.Body.String(), "data: x\n\n"; got != want {
			t.Errorf("handler.SSE{Event: %q} wrote %q, want %q", name, got, want)
		}
	}
}
