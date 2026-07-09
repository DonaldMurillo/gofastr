package stream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// SSEBroker fans out SSE events to multiple HTTP subscribers.
// Each subscriber gets a buffered channel. Default subscribers drop the
// oldest queued event when the buffer is full; clients that opt into
// ?slow=block or X-SSE-Slow: block instead backpressure Publish until
// buffer space is available.
//
// Buffer size is configurable per-subscriber via query param (?buffer=128)
// or header (X-SSE-Buffer), with a default fallback bounded by MaxBuf.
//
// When SSEBrokerConfig.Fanout is set, Publish also broadcasts to other
// replicas (topic "gofastr.sse.<Topic>") and events from other replicas are
// delivered locally. Delivery is lossy best-effort (the real-time lane).
type SSEBroker struct {
	mu                sync.RWMutex
	subscribers       map[string]*subscriber
	topic             string
	defaultBuf        int
	maxBuf            int
	heartbeatInterval time.Duration

	// fanout, when non-nil, mirrors Publish to other replicas and
	// re-delivers theirs locally. nodeID drops own-node echoes. fanoutSend
	// is the non-blocking enqueue into the publish queue
	// (fanout.PublishQueue) — Publish is called from request/emit paths and
	// must never wait on the backend's network/DB round-trip. Guarded by
	// the fanout being attached once at construction and torn down by
	// Close; reads happen only on the Publish path (mu-free after attach).
	fanout       fanout.Fanout
	nodeID       string
	fanoutTopic  string
	fanoutSend   func([]byte)
	fanoutCancel func()
	fanoutOnce   sync.Once
	closed       atomic.Bool
}

type subscriber struct {
	ch       chan sseEvent
	filter   string // optional event name filter
	done     chan struct{}
	slowMode sseSlowMode
}

type sseEvent struct {
	Name string
	Data string
	ID   string
}

type sseSlowMode uint8

const (
	sseSlowDropOldest sseSlowMode = iota
	sseSlowBlock
)

// SSEBrokerConfig configures the broker.
type SSEBrokerConfig struct {
	Topic             string        // logical topic name (for logging/debugging)
	DefaultBuf        int           // default subscriber buffer size (0 = 64)
	MaxBuf            int           // maximum allowed subscriber buffer (0 = 1024)
	HeartbeatInterval time.Duration // 0 = 30s; emits a comment frame to keep idle connections open

	// Fanout, when set, makes Publish cross replicas (topic
	// "gofastr.sse.<Topic>") and re-delivers other replicas' events locally.
	// Remote-origin events are delivered with ALWAYS drop-oldest semantics
	// (even to slow=block subscribers): a remote replica cannot be
	// backpressured through a channel send, so a single stalled subscriber
	// must never wedge receive for the others. Local Publish keeps its
	// block-mode backpressure contract. Optional. Close cancels the
	// subscription.
	Fanout fanout.Fanout
}

// NewSSEBroker creates a new broker for fan-out SSE delivery.
func NewSSEBroker(cfg SSEBrokerConfig) *SSEBroker {
	defaultBuf := cfg.DefaultBuf
	if defaultBuf <= 0 {
		defaultBuf = 64
	}
	maxBuf := cfg.MaxBuf
	if maxBuf <= 0 {
		maxBuf = 1024
	}
	if defaultBuf > maxBuf {
		defaultBuf = maxBuf
	}
	hb := cfg.HeartbeatInterval
	if hb == 0 {
		hb = 30 * time.Second
	}
	b := &SSEBroker{
		subscribers:       make(map[string]*subscriber),
		topic:             cfg.Topic,
		defaultBuf:        defaultBuf,
		maxBuf:            maxBuf,
		heartbeatInterval: hb,
		fanoutTopic:       "gofastr.sse." + cfg.Topic,
	}
	b.attachFanout(cfg.Fanout)
	return b
}

// sseFanoutMsg is the wire shape for a fanned-out SSE event.
type sseFanoutMsg struct {
	Name string `json:"n"`
	Data string `json:"d"`
	ID   string `json:"i,omitempty"`
}

// attachFanout subscribes to the broker's fanout topic so events published on
// other replicas are re-delivered locally. Own-node messages are dropped.
// Received events are NEVER re-published. A subscribe failure falls back to
// local-only with a logged warning (best-effort lane).
func (b *SSEBroker) attachFanout(f fanout.Fanout) {
	if f == nil {
		return
	}
	nodeID := fanout.NewNodeID()
	cancel, err := f.Subscribe(b.fanoutTopic, func(raw []byte) {
		origin, body, uerr := fanout.Unwrap(raw)
		if uerr != nil {
			return
		}
		if origin == nodeID {
			return // own-node: drop
		}
		var msg sseFanoutMsg
		if jerr := json.Unmarshal(body, &msg); jerr != nil {
			return
		}
		// Deliver locally only via the always-non-blocking path; never
		// re-publish on receive. (A remote origin cannot be backpressured;
		// see deliverFromFanout.)
		b.deliverFromFanout(msg.Name, msg.Data, msg.ID)
	})
	if err != nil {
		slog.Default().Warn("stream: SSEBroker fanout subscribe failed; operating local-only",
			"topic", b.fanoutTopic, "err", err)
		return
	}
	send, stopQueue := fanout.PublishQueue(f, b.fanoutTopic, 0)
	b.fanout = f
	b.nodeID = nodeID
	b.fanoutSend = send
	b.fanoutCancel = func() {
		cancel()
		stopQueue()
	}
}

// maxSubscriberID caps the length of a client-supplied subscriber_id to
// prevent unbounded key growth in the subscribers map.
const maxSubscriberID = 256

// Subscribe adds a subscriber and blocks, writing events to the response.
// The subscriber ID is taken from ?subscriber_id or X-Subscriber-ID header.
// Buffer size from ?buffer= or X-SSE-Buffer header, clamped to MaxBuf.
// Subscribe returns when the request context is canceled or the client
// disconnects.
func (b *SSEBroker) Subscribe(w http.ResponseWriter, r *http.Request) {
	bufSize := b.defaultBuf
	if v := r.URL.Query().Get("buffer"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			bufSize = n
		}
	} else if v := r.Header.Get("X-SSE-Buffer"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			bufSize = n
		}
	}
	if bufSize > b.maxBuf {
		bufSize = b.maxBuf
	}
	if bufSize <= 0 {
		bufSize = b.defaultBuf
	}

	subID := r.URL.Query().Get("subscriber_id")
	if subID == "" {
		subID = r.Header.Get("X-Subscriber-ID")
	}
	if subID == "" {
		subID = generateSubscriberID()
	}
	// Reject oversized client-supplied subscriber IDs by truncating.
	if len(subID) > maxSubscriberID {
		subID = subID[:maxSubscriberID]
	}

	filter := r.URL.Query().Get("event")

	sub := &subscriber{
		ch:       make(chan sseEvent, bufSize),
		filter:   filter,
		done:     make(chan struct{}),
		slowMode: parseSlowMode(r),
	}

	// Register, evicting any prior subscriber with the same ID so it does
	// not leak. Closing the prior sub.done signals its Subscribe loop to
	// exit cleanly.
	b.mu.Lock()
	if prev, ok := b.subscribers[subID]; ok {
		close(prev.done)
	}
	b.subscribers[subID] = sub
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		// Only delete if the map still points to *this* subscriber. If a
		// later Subscribe with the same ID has already overwritten us,
		// our done channel was closed by that path; do not clobber the
		// newer entry.
		if cur, ok := b.subscribers[subID]; ok && cur == sub {
			delete(b.subscribers, subID)
			// Close done so any delivery goroutine blocked on a
			// slow=block send (select on <-sub.done) unblocks. Without
			// this a slow=block subscriber whose loop has exited leaves
			// deliverLocal wedged forever on the channel send, which in
			// turn wedges fanout receive for every OTHER subscriber.
			//
			// Double-close is impossible: the eviction path (same subID
			// re-Subscribe above) closes prev.done AND overwrites the map
			// entry in the same critical section, so by the time this
			// defer observes cur==sub no eviction occurred; and if an
			// eviction DID occur cur!=sub and we skip. The two paths are
			// thus mutually exclusive on done ownership.
			close(sub.done)
		}
		b.mu.Unlock()
	}()

	sse := NewSSEWriter(w)
	sse.ensureHeaders()
	sse.Flush()

	ctxDone := r.Context().Done()

	var hbCh <-chan time.Time
	if b.heartbeatInterval > 0 {
		t := time.NewTicker(b.heartbeatInterval)
		defer t.Stop()
		hbCh = t.C
	}

	for {
		select {
		case <-ctxDone:
			return
		case <-sub.done:
			return
		case <-hbCh:
			if err := sse.WriteComment("heartbeat"); err != nil {
				return
			}
		case evt, ok := <-sub.ch:
			if !ok {
				return
			}
			if sub.filter != "" && evt.Name != sub.filter {
				continue
			}
			if evt.ID != "" {
				sse.SetID(evt.ID)
			}
			if err := sse.WriteEvent(evt.Name, evt.Data); err != nil {
				return
			}
		}
	}
}

// Publish sends an event to all subscribers. If a default subscriber's
// buffer is full, the oldest event is dropped. A subscriber that opted into
// slow=block backpressures this call until buffer space opens or that
// subscriber is closed. Subscribers are snapshotted under the read lock;
// sends happen outside the lock to keep fan-out from holding the broker
// lock during slow per-channel writes.
func (b *SSEBroker) Publish(name, data string, id ...string) {
	var eventID string
	if len(id) > 0 {
		eventID = id[0]
	}
	b.deliverLocal(name, data, eventID)
	b.publishFanout(name, data, eventID)
}

// deliverLocal sends a locally-originated event to every local subscriber
// with the broker's drop-oldest / block semantics: a slow=block subscriber
// backpressures this call (block-mode backpressure is a LOCAL Publish
// contract — the local emitter chose to publish and can be stalled).
func (b *SSEBroker) deliverLocal(name, data, eventID string) {
	b.deliver(name, data, eventID, false)
}

// deliverFromFanout sends a remote-origin event to every local subscriber
// with ALWAYS drop-oldest semantics, even for slow=block subscribers. A
// remote replica cannot be backpressured through a channel send: if a single
// slow=block subscriber on this replica stopped reading, blocking on its
// channel would wedge fanout receive for ALL other (healthy) subscribers on
// this replica. The fanout-receive lane must therefore never block.
func (b *SSEBroker) deliverFromFanout(name, data, eventID string) {
	b.deliver(name, data, eventID, true)
}

// deliver fans evt out to every local subscriber. When fromFanout is false
// (local Publish) slow=block subscribers backpressure via select-on-done;
// when fromFanout is true (remote origin) every subscriber gets drop-oldest
// so the receive lane can never wedge.
func (b *SSEBroker) deliver(name, data, eventID string, fromFanout bool) {
	evt := sseEvent{Name: name, Data: data, ID: eventID}

	b.mu.RLock()
	subs := make([]*subscriber, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subs = append(subs, sub)
	}
	b.mu.RUnlock()

	for _, sub := range subs {
		if !fromFanout && sub.slowMode == sseSlowBlock {
			select {
			case sub.ch <- evt:
			case <-sub.done:
			}
			continue
		}
		select {
		case sub.ch <- evt:
		default:
			// Buffer full — drop oldest and try again
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- evt:
			default:
				// Still full after drain — drop entirely
			}
		}
	}
}

// publishFanout mirrors the event to other replicas via the attached fanout,
// if any. Best-effort; no-op without a fanout. The enqueue never blocks —
// Publish runs on request/emit goroutines and a stalled backend must not
// stall them (see fanout.PublishQueue).
func (b *SSEBroker) publishFanout(name, data, eventID string) {
	if b.fanoutSend == nil || b.closed.Load() {
		return
	}
	body, err := json.Marshal(sseFanoutMsg{Name: name, Data: data, ID: eventID})
	if err != nil {
		return
	}
	b.fanoutSend(fanout.Wrap(b.nodeID, body))
}

// Close tears down the broker's fanout participation entirely: the receive
// subscription is cancelled and subsequent Publish calls stop crossing
// replicas. It is a no-op when no fanout is attached. The broker has no other
// goroutines to stop — its subscribers live and die with their HTTP request
// contexts. Safe to call multiple times.
func (b *SSEBroker) Close() {
	b.fanoutOnce.Do(func() {
		b.closed.Store(true)
		if b.fanoutCancel != nil {
			b.fanoutCancel()
		}
	})
}

func parseSlowMode(r *http.Request) sseSlowMode {
	if r.URL.Query().Get("slow") == "block" || r.Header.Get("X-SSE-Slow") == "block" {
		return sseSlowBlock
	}
	return sseSlowDropOldest
}

// SubscriberCount returns the number of active subscribers.
func (b *SSEBroker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// generateSubscriberID returns 16 random bytes hex-encoded (32 chars).
// Unguessable, collision-resistant, no global counter contention.
func generateSubscriberID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Extremely unlikely; fall back to time-based id rather than panic.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf[:])
}
