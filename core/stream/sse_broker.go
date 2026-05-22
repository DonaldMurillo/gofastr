package stream

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// SSEBroker fans out SSE events to multiple HTTP subscribers.
// Each subscriber gets a buffered channel; when the buffer is full,
// old events are dropped (backpressure).
//
// Buffer size is configurable per-subscriber via query param (?buffer=128)
// or header (X-SSE-Buffer), with a default fallback bounded by MaxBuf.
type SSEBroker struct {
	mu                sync.RWMutex
	subscribers       map[string]*subscriber
	topic             string
	defaultBuf        int
	maxBuf            int
	heartbeatInterval time.Duration
}

type subscriber struct {
	ch     chan sseEvent
	filter string // optional event name filter
	done   chan struct{}
}

type sseEvent struct {
	Name string
	Data string
	ID   string
}

// SSEBrokerConfig configures the broker.
type SSEBrokerConfig struct {
	Topic             string        // logical topic name (for logging/debugging)
	DefaultBuf        int           // default subscriber buffer size (0 = 64)
	MaxBuf            int           // maximum allowed subscriber buffer (0 = 1024)
	HeartbeatInterval time.Duration // 0 = 30s; emits a comment frame to keep idle connections open
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
	return &SSEBroker{
		subscribers:       make(map[string]*subscriber),
		topic:             cfg.Topic,
		defaultBuf:        defaultBuf,
		maxBuf:            maxBuf,
		heartbeatInterval: hb,
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
		ch:     make(chan sseEvent, bufSize),
		filter: filter,
		done:   make(chan struct{}),
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

// Publish sends an event to all subscribers. If a subscriber's buffer
// is full, the oldest event is dropped (backpressure with delivery ratio
// tracking via the broker's metrics). Subscribers are snapshotted under
// the read lock; sends happen outside the lock to keep fan-out from
// holding the broker lock during slow per-channel writes.
func (b *SSEBroker) Publish(name, data string, id ...string) {
	var eventID string
	if len(id) > 0 {
		eventID = id[0]
	}

	evt := sseEvent{Name: name, Data: data, ID: eventID}

	b.mu.RLock()
	subs := make([]*subscriber, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subs = append(subs, sub)
	}
	b.mu.RUnlock()

	for _, sub := range subs {
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
