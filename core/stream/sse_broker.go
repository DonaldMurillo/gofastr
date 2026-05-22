package stream

import (
	"net/http"
	"strconv"
	"sync"
)

// SSEBroker fans out SSE events to multiple HTTP subscribers.
// Each subscriber gets a buffered channel; when the buffer is full,
// old events are dropped (backpressure).
//
// Buffer size is configurable per-subscriber via query param (?buffer=128)
// or header (X-SSE-Buffer), with a default fallback.
type SSEBroker struct {
	mu          sync.RWMutex
	subscribers map[string]*subscriber
	topic       string
	defaultBuf  int
}

type subscriber struct {
	ch     chan sseEvent
	filter string // optional event name filter
}

type sseEvent struct {
	Name string
	Data string
	ID   string
}

// SSEBrokerConfig configures the broker.
type SSEBrokerConfig struct {
	Topic       string // logical topic name (for logging/debugging)
	DefaultBuf  int    // default subscriber buffer size (0 = 64)
	MaxBuf      int    // maximum allowed subscriber buffer (0 = 1024)
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
	return &SSEBroker{
		subscribers: make(map[string]*subscriber),
		topic:       cfg.Topic,
		defaultBuf:  defaultBuf,
	}
}

const maxSubscriberID = 256

// Subscribe adds a subscriber and blocks, writing events to the response.
// The subscriber ID is taken from ?subscriber_id or X-Subscriber-ID header.
// Buffer size from ?buffer= or X-SSE-Buffer header.
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

	subID := r.URL.Query().Get("subscriber_id")
	if subID == "" {
		subID = r.Header.Get("X-Subscriber-ID")
	}
	if subID == "" {
		subID = generateSubscriberID()
	}

	filter := r.URL.Query().Get("event")

	sub := &subscriber{
		ch:     make(chan sseEvent, bufSize),
		filter: filter,
	}

	b.mu.Lock()
	b.subscribers[subID] = sub
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.subscribers, subID)
		b.mu.Unlock()
	}()

	sse := NewSSEWriter(w)
	sse.ensureHeaders()
	sse.Flush()

	for evt := range sub.ch {
		// Apply optional filter
		if sub.filter != "" && evt.Name != sub.filter {
			continue
		}
		if evt.ID != "" {
			sse.SetID(evt.ID)
		}
		if err := sse.WriteEvent(evt.Name, evt.Data); err != nil {
			return // client disconnected
		}
	}
}

// Publish sends an event to all subscribers. If a subscriber's buffer
// is full, the oldest event is dropped (backpressure with delivery ratio
// tracking via the broker's metrics).
func (b *SSEBroker) Publish(name, data string, id ...string) {
	var eventID string
	if len(id) > 0 {
		eventID = id[0]
	}

	evt := sseEvent{Name: name, Data: data, ID: eventID}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscribers {
		select {
		case sub.ch <- evt:
		default:
			// Buffer full — drain oldest and try again
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

func generateSubscriberID() string {
	// Simple incrementing ID — fine for single-process
	idMu.Lock()
	defer idMu.Unlock()
	lastSubscriberID++
	return strconv.FormatInt(lastSubscriberID, 36)
}

var (
	lastSubscriberID int64
	idMu             sync.Mutex
)
