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

	// allowClientSlowMode / blockTO / maxSubs mirror the config; see
	// SSEBrokerConfig for why each defaults the way it does.
	allowClientSlowMode bool
	blockTO             time.Duration
	maxSubs             int

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

	// principal resolves a request to a caller identity for
	// subscriber-id eviction scoping. nil disables eviction.
	principal func(*http.Request) string
}

type subscriber struct {
	// requestedID is the stable client-supplied id. The map key may be a
	// generated id when callers collide without a trustworthy principal.
	requestedID string
	// principal identifies the caller that registered this subscriber,
	// so a client-supplied subscriber_id can only evict an entry the
	// same caller created. See Subscribe.
	principal string
	ch        chan sseEvent
	filter    string // optional event name filter
	done      chan struct{}
	slowMode  sseSlowMode
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

	// AllowClientSlowMode lets a REQUEST select block mode via
	// ?slow=block / X-SSE-Slow. Off by default.
	//
	// deliver() walks subscribers sequentially on the publisher's
	// goroutine, so a block-mode subscriber that stops reading stalls
	// every other subscriber AND whatever called Publish — usually a
	// request handler. On a public endpoint that is an unauthenticated
	// denial of service, so the choice belongs to the developer who
	// knows whether the endpoint is trusted, not to the caller.
	AllowClientSlowMode bool

	// BlockTimeout bounds how long a block-mode send may stall before
	// the broker gives up on that subscriber and moves on. 0 = 5s.
	// A blocking send with no timeout is unbounded backpressure.
	BlockTimeout time.Duration

	// Principal identifies the caller behind a request, so a reconnect
	// with the same ?subscriber_id replaces its OWN entry and nobody
	// else's. Return "" for "cannot tell", which is treated as "not the
	// same caller".
	//
	// Set this to something the caller cannot choose and another caller
	// cannot guess — a session user id is the usual answer.
	//
	// Left nil, the broker never evicts. The old default keyed on
	// RemoteAddr's host, which is honest only on a direct connection:
	// behind nginx, an ALB, Cloudflare or a k8s ingress, every request's
	// TCP peer is the proxy, so all subscribers collapsed to one
	// principal and `?subscriber_id=<victim>` dropped the victim's
	// stream — repeatably. Nothing is lost by not evicting: subscriber
	// ids address nothing (deliver() broadcasts), and a dropped
	// connection already unregisters itself.
	Principal func(*http.Request) string

	// MaxSubscribers caps concurrent subscribers; 0 = unlimited. Subscribe
	// rejects past the cap rather than evicting, and the cap is exact.
	//
	// A client whose previous connection is half-open (mobile handoff, laptop
	// sleep, an LB idle-kill the server has not noticed) still holds a seat
	// until HeartbeatInterval's next write fails and the stream unregisters
	// itself. That heartbeat is what reclaims the seat — for every client,
	// including the ones that send no subscriber_id, which is all of the
	// framework's own. A reserved slot keyed on the requested id was tried
	// and removed: nothing in the client runtime sends an id, so it could
	// never fire, while costing a scan of every subscriber under the write
	// lock and letting the cap be exceeded by one.
	MaxSubscribers int
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
		principal:         cfg.Principal,

		allowClientSlowMode: cfg.AllowClientSlowMode,
		blockTO:             cfg.BlockTimeout,
		maxSubs:             cfg.MaxSubscribers,
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
		requestedID: subID,
		ch:          make(chan sseEvent, bufSize),
		filter:      filter,
		done:        make(chan struct{}),
		slowMode:    b.parseSlowMode(r),
	}

	// Eviction is scoped to the SAME principal. subscriber_id exists so
	// apps can pass a meaningful id (user, tab, device), but it is
	// attacker-supplied, and evicting on a bare id match let
	// `?subscriber_id=<victim>` drop the victim's stream. Without a
	// Principal, colliding callers therefore keep separate map entries.
	sub.principal = b.principalFor(r)

	b.mu.Lock()
	registrationID := subID
	var prev *subscriber
	if sub.principal != "" {
		// Only a caller the host can identify may replace a stream. A bare
		// subscriber_id is attacker-supplied, so colliding anonymous callers
		// keep separate map entries rather than evicting each other.
		for id, incumbent := range b.subscribers {
			if incumbent.requestedID == sub.requestedID && incumbent.principal == sub.principal {
				registrationID = id
				prev = incumbent
				break
			}
		}
	}

	replacing := prev != nil
	if !replacing {
		if _, used := b.subscribers[registrationID]; used {
			for {
				registrationID = generateSubscriberID()
				if _, exists := b.subscribers[registrationID]; !exists {
					break
				}
			}
		}
	}

	if b.maxSubs > 0 && !replacing && len(b.subscribers) >= b.maxSubs {
		b.mu.Unlock()
		http.Error(w, "too many subscribers", http.StatusServiceUnavailable)
		return
	}
	if replacing {
		close(prev.done)
	}
	b.subscribers[registrationID] = sub
	b.mu.Unlock()
	subID = registrationID

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
			// Bounded: an unbounded send lets one stalled subscriber
			// wedge deliver() — and therefore every other subscriber
			// and the calling handler — for as long as it likes.
			timer := time.NewTimer(b.blockTimeout())
			select {
			case sub.ch <- evt:
			case <-sub.done:
			case <-timer.C:
			}
			timer.Stop()
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

// parseSlowMode reads the client's requested slow-consumer mode. The
// request may only select block mode when the host opted in via
// SSEBrokerConfig.AllowClientSlowMode — see that field for why.
func (b *SSEBroker) parseSlowMode(r *http.Request) sseSlowMode {
	if !b.allowClientSlowMode {
		return sseSlowDropOldest
	}
	if r.URL.Query().Get("slow") == "block" || r.Header.Get("X-SSE-Slow") == "block" {
		return sseSlowBlock
	}
	return sseSlowDropOldest
}

// principalFor derives the caller identity used to scope subscriber-id
// eviction, or "" when the host gave the broker no way to tell callers
// apart. "" is never treated as equal to another principal, so it
// disables eviction rather than widening it.
func (b *SSEBroker) principalFor(r *http.Request) string {
	if b.principal == nil || r == nil {
		return ""
	}
	return b.principal(r)
}

// blockTimeout is the bound on a block-mode send. Never zero, so a
// blocking send can always make progress.
func (b *SSEBroker) blockTimeout() time.Duration {
	if b.blockTO > 0 {
		return b.blockTO
	}
	return 5 * time.Second
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
