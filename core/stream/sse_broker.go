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
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// SSEBroker fans SSE events out to the HTTP subscribers it holds and, with
// a Fanout, to and from other replicas.
type SSEBroker struct {
	mu                sync.RWMutex
	subscribers       map[string]*subscriber
	topic             string
	defaultBuf        int
	maxBuf            int
	heartbeatInterval time.Duration

	allowClientSlowMode bool
	blockTO             time.Duration
	maxSubs             int

	fanout       fanout.Fanout
	nodeID       string
	fanoutTopic  string
	fanoutSend   func([]byte)
	fanoutCancel func()
	fanoutOnce   sync.Once
	closed       atomic.Bool

	principal func(*http.Request) string

	seatCap      int
	seatOverflow SeatOverflowPolicy
	seatOrder    map[string][]*subscriber
}

type subscriber struct {
	requestedID string
	principal   string
	regID       string
	ch          chan sseEvent
	filter      string
	done        chan struct{}
	slowMode    sseSlowMode

	cutMu    sync.Mutex
	cut      func()
	returned bool
}

func (s *subscriber) interruptWrite() {
	s.cutMu.Lock()
	defer s.cutMu.Unlock()
	if !s.returned && s.cut != nil {
		s.cut()
	}
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

	// Fanout, when set, makes Publish cross replicas on topic
	// "gofastr.sse.<Topic>" and delivers other replicas' events locally.
	// Close cancels the subscription.
	Fanout fanout.Fanout

	// AllowClientSlowMode lets a request choose block mode with ?slow=block
	// or X-SSE-Slow: block. Off by default.
	AllowClientSlowMode bool

	// BlockTimeout bounds one block-mode send. 0 = 5s.
	BlockTimeout time.Duration

	// Principal names the caller behind a request; a reconnect with the same
	// ?subscriber_id and principal replaces its own stream. "" means "cannot
	// tell" and never matches. Nil disables replacement.
	Principal func(*http.Request) string

	// MaxSeatsPerPrincipal caps the streams one principal holds. 0 = 16,
	// negative = no cap. Callers Principal cannot tell apart share one bucket.
	MaxSeatsPerPrincipal int

	// SeatOverflow picks what a principal at MaxSeatsPerPrincipal gets:
	// SeatOverflowRefuse (429, the default) or SeatOverflowEvictOldest.
	SeatOverflow SeatOverflowPolicy

	// MaxSubscribers caps concurrent subscribers; 0 = unlimited. Past the cap
	// Subscribe answers 503.
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
		seatCap:             cfg.MaxSeatsPerPrincipal,
		seatOverflow:        cfg.SeatOverflow,
		seatOrder:           make(map[string][]*subscriber),
	}
	b.attachFanout(cfg.Fanout)
	return b
}

type sseFanoutMsg struct {
	Name string `json:"n"`
	Data string `json:"d"`
	ID   string `json:"i,omitempty"`
}

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
			return
		}
		var msg sseFanoutMsg
		if jerr := json.Unmarshal(body, &msg); jerr != nil {
			return
		}
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

const maxSubscriberID = 256

// Subscribe writes events to w until the request ends or the stream is
// evicted. It reads ?subscriber_id (or X-Subscriber-ID), ?buffer= (or
// X-SSE-Buffer, clamped to MaxBuf) and the ?event= filter.
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
	if len(subID) > maxSubscriberID {
		subID = subID[:maxSubscriberID]
	}

	filter := r.URL.Query().Get("event")

	rc := http.NewResponseController(w)
	sub := &subscriber{
		requestedID: subID,
		ch:          make(chan sseEvent, bufSize),
		filter:      filter,
		done:        make(chan struct{}),
		slowMode:    b.parseSlowMode(r),
		cut:         func() { _ = rc.SetWriteDeadline(time.Now()) },
	}
	defer func() {
		sub.cutMu.Lock()
		sub.returned = true
		sub.cutMu.Unlock()
	}()

	sub.principal = b.principalFor(r)

	b.mu.Lock()
	registrationID := subID
	var prev *subscriber
	if sub.principal != "" {
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
		i18n.HTTPError(w, r, http.StatusServiceUnavailable, reasonTooManySubscribers)
		return
	}
	var evicted *subscriber
	if !replacing {
		if seats := seatsFor(b.seatCap); seats > 0 && len(b.seatOrder[sub.principal]) >= seats {
			if b.seatOverflow != SeatOverflowEvictOldest || len(b.seatOrder[sub.principal]) == 0 {
				b.mu.Unlock()
				i18n.HTTPError(w, r, http.StatusTooManyRequests, i18n.ReasonTooManyStreams)
				return
			}
			oldest := b.seatOrder[sub.principal][0]
			b.spliceSeatLocked(oldest)
			delete(b.subscribers, oldest.regID)
			close(oldest.done)
			evicted = oldest
		}
	}
	if replacing {
		b.spliceSeatLocked(prev)
		close(prev.done)
		evicted = prev
	}
	sub.regID = registrationID
	b.subscribers[registrationID] = sub
	b.seatOrder[sub.principal] = append(b.seatOrder[sub.principal], sub)
	b.mu.Unlock()
	if evicted != nil {
		evicted.interruptWrite()
	}
	subID = registrationID

	defer func() {
		b.mu.Lock()
		b.spliceSeatLocked(sub)
		if cur, ok := b.subscribers[subID]; ok && cur == sub {
			delete(b.subscribers, subID)
			close(sub.done)
		}
		b.mu.Unlock()
	}()

	sse := NewSSEWriter(w)
	sse.ensureHeaders()
	sse.Flush()

	ctxDone := r.Context().Done()
	held := func() bool {
		_, ok := handler.RecheckPrincipal(r.Context())
		return ok
	}

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
			if !held() {
				return
			}
			if err := sse.WriteComment("heartbeat"); err != nil {
				return
			}
		case evt, ok := <-sub.ch:
			if !ok {
				return
			}
			if !sub.wants(evt.Name) {
				continue
			}
			if !held() {
				return
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

// Publish delivers an event to every local subscriber and, with a Fanout,
// to other replicas. A full default subscriber drops its oldest event; a
// block-mode subscriber holds Publish up to BlockTimeout.
func (b *SSEBroker) Publish(name, data string, id ...string) {
	var eventID string
	if len(id) > 0 {
		eventID = id[0]
	}
	b.deliverLocal(name, data, eventID)
	b.publishFanout(name, data, eventID)
}

func (b *SSEBroker) deliverLocal(name, data, eventID string) {
	b.deliver(name, data, eventID, false)
}

func (b *SSEBroker) deliverFromFanout(name, data, eventID string) {
	b.deliver(name, data, eventID, true)
}

func (b *SSEBroker) deliver(name, data, eventID string, fromFanout bool) {
	evt := sseEvent{Name: name, Data: data, ID: eventID}

	b.mu.RLock()
	subs := make([]*subscriber, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subs = append(subs, sub)
	}
	b.mu.RUnlock()

	for _, sub := range subs {
		if !sub.wants(name) {
			continue
		}
		if !fromFanout && sub.slowMode == sseSlowBlock {
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
			select {
			case <-sub.ch:
			default:
			}
			select {
			case sub.ch <- evt:
			default:
			}
		}
	}
}

func (s *subscriber) wants(name string) bool {
	return s.filter == "" || name == s.filter
}

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

// Close ends the broker's fanout participation: the receive subscription
// is cancelled and later Publish calls stay local. Safe to call twice.
func (b *SSEBroker) Close() {
	b.fanoutOnce.Do(func() {
		b.closed.Store(true)
		if b.fanoutCancel != nil {
			b.fanoutCancel()
		}
	})
}

func (b *SSEBroker) parseSlowMode(r *http.Request) sseSlowMode {
	if !b.allowClientSlowMode {
		return sseSlowDropOldest
	}
	if r.URL.Query().Get("slow") == "block" || r.Header.Get("X-SSE-Slow") == "block" {
		return sseSlowBlock
	}
	return sseSlowDropOldest
}

func (b *SSEBroker) principalFor(r *http.Request) string {
	if b.principal == nil || r == nil {
		return ""
	}
	return b.principal(r)
}

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

func generateSubscriberID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("stream: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(buf[:])
}
