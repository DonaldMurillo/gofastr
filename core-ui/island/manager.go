package island

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// IslandUpdate represents an update to push to a client.
type IslandUpdate struct {
	IslandID string
	HTML     string
}

type streamEntry struct {
	subs map[uint64]chan IslandUpdate // subscriber id → its buffered channel
}

type streamCaps struct {
	perSession int // max streams sharing one session id (one per tab/connection)
	perPeer    int // max streams one client holds across all its sessions (see ClientKey)
	global     int // max streams across all sessions on this replica
}

// Default concurrent SSE stream ceilings; override with WithStreamCaps and WithPeerStreamCap.
const (
	DefaultStreamsPerSession = 16
	DefaultStreamsPerPeer    = 256
	DefaultGlobalStreams     = 4096
)

// Default SSE keepalive period and stream lifetime; override with WithSSEHeartbeat and WithSSEStreamBound.
const (
	DefaultSSEHeartbeat   = 15 * time.Second
	DefaultSSEStreamBound = 5 * time.Minute
)

// ErrSessionStreamLimit is returned when a session already holds its per-session cap of concurrent
// SSE streams.
var ErrSessionStreamLimit = errors.New("island: per-session SSE stream limit reached")

// ErrGlobalStreamLimit is returned when the replica already holds its global cap of concurrent SSE
// streams.
var ErrGlobalStreamLimit = errors.New("island: global SSE stream limit reached")

// ErrPeerStreamLimit is returned when one client (see ClientKey) already holds its per-client cap
// of concurrent SSE streams across its sessions.
var ErrPeerStreamLimit = errors.New("island: per-client SSE stream limit reached")

// ManagerOption configures a Manager at construction (NewManager).
type ManagerOption func(*Manager)

// WithStreamCaps overrides the concurrent-SSE-stream ceilings.
func WithStreamCaps(perSession, global int) ManagerOption {
	return func(m *Manager) {
		m.caps.perSession = perSession
		m.caps.global = global
	}
}

// WithPeerStreamCap overrides the per-client stream ceiling (DefaultStreamsPerPeer): how many
// streams one client may hold open across every session it has minted.
func WithPeerStreamCap(n int) ManagerOption {
	return func(m *Manager) {
		m.caps.perPeer = n
	}
}

// WithTrustedProxies names the reverse proxies (IPs or CIDRs) whose X-Forwarded-For an anonymous
// stream's client address is read from.
func WithTrustedProxies(entries ...string) ManagerOption {
	return func(m *Manager) {
		m.proxies = clientip.ParseProxies(entries)
		m.proxiesSet = len(entries) > 0
	}
}

// WithSSEHeartbeat sets the interval between SSE keepalive comment frames (": ping") written on an
// idle stream (default DefaultSSEHeartbeat = 15s).
func WithSSEHeartbeat(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.sseHeartbeat = d
		}
	}
}

// WithSSEStreamBound sets the maximum lifetime of a single SSE stream (default
// DefaultSSEStreamBound = 5m).
func WithSSEStreamBound(d time.Duration) ManagerOption {
	return func(m *Manager) {
		if d > 0 {
			m.sseStreamBound = d
		}
	}
}

// Manager tracks active islands across all client sessions.
type Manager struct {
	mu      sync.RWMutex
	streams map[string]*streamEntry // sessionID → update stream

	caps       streamCaps
	globalSubs int
	peerSubs   map[string]int
	proxies    clientip.Proxies
	proxiesSet bool
	proxyHint  sync.Once // hintUntrustedProxy logs once per manager

	sseHeartbeat   time.Duration
	sseStreamBound time.Duration

	presenceConns  map[uint64]*presenceConn
	nextPresenceID atomic.Uint64
	nextSubID      uint64 // stream-subscriber id source; guarded by mu

	onPresenceChange atomic.Pointer[func(string)]

	authorizeTopic atomic.Pointer[func(context.Context, string) bool]

	dropped atomic.Int64

	fanout     fanout.Fanout
	nodeID     string
	fanoutSend func([]byte)

	presenceSend      func([]byte)
	remoteRosters     map[string]map[string]remoteRosterEntry
	remoteRosterTouch map[string]time.Time
	presenceHeartbeat time.Duration
	presenceTTL       time.Duration
	presenceDone      chan struct{}
	presenceWG        sync.WaitGroup
}

// DroppedUpdates returns the cumulative number of island updates dropped because a client's SSE
// channel was full.
func (m *Manager) DroppedUpdates() int64 { return m.dropped.Load() }

// SetOnPresenceChange installs the roster-change callback.
func (m *Manager) SetOnPresenceChange(fn func(topic string)) {
	if fn == nil {
		m.onPresenceChange.Store(nil)
		return
	}
	m.onPresenceChange.Store(&fn)
}

// SetAuthorizeTopic installs the topic-authorization gate.
func (m *Manager) SetAuthorizeTopic(fn func(ctx context.Context, topic string) bool) {
	if fn == nil {
		m.authorizeTopic.Store(nil)
		return
	}
	m.authorizeTopic.Store(&fn)
}

func (m *Manager) presenceCallback() func(string) {
	if p := m.onPresenceChange.Load(); p != nil {
		return *p
	}
	return nil
}

func (m *Manager) topicAuthorizer() func(context.Context, string) bool {
	if p := m.authorizeTopic.Load(); p != nil {
		return *p
	}
	return nil
}

// NewManager creates a new island manager.
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		streams:  make(map[string]*streamEntry),
		peerSubs: make(map[string]int),
		caps: streamCaps{
			perSession: DefaultStreamsPerSession,
			perPeer:    DefaultStreamsPerPeer,
			global:     DefaultGlobalStreams,
		},
		sseHeartbeat:   DefaultSSEHeartbeat,
		sseStreamBound: DefaultSSEStreamBound,
	}
	for _, o := range opts {
		o(m)
	}
	if m.sseStreamBound <= m.sseHeartbeat {
		m.sseStreamBound = DefaultSSEStreamBound
	}
	if m.sseStreamBound <= m.sseHeartbeat {
		m.sseStreamBound = m.sseHeartbeat + DefaultSSEStreamBound
	}
	return m
}

// Subscribe registers a new subscriber on the session's stream and returns its OWN buffered update
// channel plus a cancel func that removes exactly this subscription (idempotent).
func (m *Manager) Subscribe(sessionID string) (<-chan IslandUpdate, func()) {
	ch, cancel, err := m.subscribeImpl(sessionID, "")
	if err != nil {
		refused := make(chan IslandUpdate)
		close(refused)
		return refused, cancel
	}
	return ch, cancel
}

func (m *Manager) subscribeImpl(sessionID, peer string) (<-chan IslandUpdate, func(), error) {
	m.mu.Lock()
	entry, ok := m.streams[sessionID]
	if m.caps.perSession > 0 && ok && len(entry.subs) >= m.caps.perSession {
		m.mu.Unlock()
		return nil, func() {}, ErrSessionStreamLimit
	}
	if m.caps.perPeer > 0 && m.peerSubs[peer] >= m.caps.perPeer {
		m.mu.Unlock()
		return nil, func() {}, ErrPeerStreamLimit
	}
	if m.caps.global > 0 && m.globalSubs >= m.caps.global {
		m.mu.Unlock()
		return nil, func() {}, ErrGlobalStreamLimit
	}
	if !ok {
		entry = &streamEntry{subs: make(map[uint64]chan IslandUpdate)}
		m.streams[sessionID] = entry
	}
	m.nextSubID++
	id := m.nextSubID
	ch := make(chan IslandUpdate, 64)
	entry.subs[id] = ch
	m.globalSubs++
	if peer != "" {
		m.peerSubs[peer]++
	}
	m.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			if e, ok := m.streams[sessionID]; ok {
				if _, sub := e.subs[id]; sub {
					delete(e.subs, id)
					m.globalSubs--
					if peer != "" {
						if m.peerSubs[peer]--; m.peerSubs[peer] <= 0 {
							delete(m.peerSubs, peer)
						}
					}
				}
				if len(e.subs) == 0 {
					delete(m.streams, sessionID)
				}
			}
		})
	}
	return ch, cancel, nil
}

// PushUpdate sends a direct update to a session's SSE stream.
func (m *Manager) PushUpdate(update IslandUpdate, sessionID string) {
	m.deliver(update, sessionID)
	m.publishFanout(sessionID, update)
}

const islandFanoutTopic = "gofastr.islands"

type islandFanoutMsg struct {
	SessionID string `json:"s"`
	IslandID  string `json:"i"`
	HTML      string `json:"h"`
}

func (m *Manager) deliver(update IslandUpdate, sessionID string) {
	m.mu.RLock()
	entry, ok := m.streams[sessionID]
	var chans []chan IslandUpdate
	if ok {
		chans = make([]chan IslandUpdate, 0, len(entry.subs))
		for _, ch := range entry.subs {
			chans = append(chans, ch)
		}
	}
	m.mu.RUnlock()
	for _, ch := range chans {
		select {
		case ch <- update:
		default:
			m.dropped.Add(1)
		}
	}
}

func (m *Manager) publishFanout(sessionID string, update IslandUpdate) {
	m.mu.RLock()
	send := m.fanoutSend
	nodeID := m.nodeID
	m.mu.RUnlock()
	if send == nil {
		return
	}
	msg := islandFanoutMsg{SessionID: sessionID, IslandID: update.IslandID, HTML: update.HTML}
	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	send(fanout.Wrap(nodeID, body))
}

// SetFanout attaches a fanout so PushUpdate updates cross replicas and updates originating on other
// replicas are re-delivered to the local session stream (topic "gofastr.islands").
func (m *Manager) SetFanout(f fanout.Fanout) (stop func(), err error) {
	detach, err := m.attachFanout(f)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = detach(context.Background()) }) }, nil
}

// SetFanoutContext is [Manager.SetFanout] with a stop function that waits only
// until ctx ends. If the caller stops waiting, detachment continues as in-flight work allows.
func (m *Manager) SetFanoutContext(f fanout.Fanout) (stop func(context.Context) error, err error) {
	detach, err := m.attachFanout(f)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	done := make(chan struct{})
	var stopErr error
	return func(ctx context.Context) error {
		once.Do(func() {
			go func() {
				defer close(done)
				defer func() {
					if r := recover(); r != nil {
						stopErr = fmt.Errorf("island fanout stop panicked: %v", textsafe.Recovered(r))
					}
				}()
				stopErr = detach(ctx)
			}()
		})
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-done:
			return stopErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil
}

func (m *Manager) attachFanout(f fanout.Fanout) (func(context.Context) error, error) {
	if f == nil {
		return nil, errors.New("island: SetFanout: nil fanout")
	}
	nodeID := fanout.NewNodeID()
	islandSend, islandStopQueue := fanout.PublishQueue(f, islandFanoutTopic, 0)
	presenceSend, presenceStopQueue := fanout.PublishQueue(f, presenceFanoutTopic, 0)

	m.mu.Lock()
	if m.fanout != nil {
		m.mu.Unlock()
		islandStopQueue()
		presenceStopQueue()
		return nil, errors.New("island: fanout already attached")
	}
	m.fanout = f
	m.nodeID = nodeID
	m.fanoutSend = islandSend
	m.presenceSend = presenceSend
	m.remoteRosters = make(map[string]map[string]remoteRosterEntry)
	m.remoteRosterTouch = make(map[string]time.Time)
	m.presenceTTL = defaultPresenceTTL
	m.presenceHeartbeat = defaultPresenceHeartbeat
	m.presenceDone = make(chan struct{})
	m.mu.Unlock()

	islandCancel, subErr := f.Subscribe(islandFanoutTopic, func(raw []byte) {
		_, body, ok := fanout.UnwrapForeign(nodeID, raw)
		if !ok {
			return
		}
		var msg islandFanoutMsg
		if jerr := json.Unmarshal(body, &msg); jerr != nil {
			return
		}
		m.deliver(IslandUpdate{IslandID: msg.IslandID, HTML: msg.HTML}, msg.SessionID)
	})
	if subErr != nil {
		islandStopQueue()
		presenceStopQueue()
		m.mu.Lock()
		m.fanout = nil
		m.nodeID = ""
		m.fanoutSend = nil
		m.clearPresenceFanoutLocked()
		m.mu.Unlock()
		return nil, fmt.Errorf("island: SetFanout: subscribe: %w", subErr)
	}

	presenceCancel, presenceSubErr := f.Subscribe(presenceFanoutTopic, func(raw []byte) {
		origin, body, ok := fanout.UnwrapForeign(nodeID, raw)
		if !ok {
			return
		}
		var msg presenceFanoutMsg
		if jerr := json.Unmarshal(body, &msg); jerr != nil {
			return
		}
		m.mergeRemotePresence(origin, msg)
	})
	if presenceSubErr != nil {
		islandCancel()
		islandStopQueue()
		presenceStopQueue()
		m.mu.Lock()
		m.fanout = nil
		m.nodeID = ""
		m.fanoutSend = nil
		m.clearPresenceFanoutLocked()
		m.mu.Unlock()
		return nil, fmt.Errorf("island: SetFanout: presence subscribe: %w", presenceSubErr)
	}

	m.mu.RLock()
	presenceDone := m.presenceDone
	m.mu.RUnlock()
	m.presenceWG.Add(1)
	go m.presenceHeartbeatLoop(presenceDone)
	m.broadcastAllLocalTopics()

	return func(ctx context.Context) error {
		m.mu.Lock()
		done := m.presenceDone
		m.presenceSend = nil
		m.mu.Unlock()
		if done != nil {
			close(done)
		}
		m.presenceWG.Wait()
		presenceStopQueue()
		m.gracefulLeaveLocalTopics(ctx, f)
		islandCancel()
		presenceCancel()
		islandStopQueue()
		m.mu.Lock()
		topics := slices.Sorted(maps.Keys(m.remoteRosters))
		before := make([][]PresenceMember, len(topics))
		for i, topic := range topics {
			before[i] = shownRosterLocked(m, topic)
		}
		m.fanout = nil
		m.nodeID = ""
		m.fanoutSend = nil
		m.clearPresenceFanoutLocked()
		var changed []string
		for i, topic := range topics {
			if !membersEqual(before[i], shownRosterLocked(m, topic)) {
				changed = append(changed, topic)
			}
		}
		cb := m.presenceCallback()
		m.mu.Unlock()
		firePresenceChange(cb, changed)
		return ctx.Err()
	}, nil
}

func (m *Manager) clearPresenceFanoutLocked() {
	m.presenceSend = nil
	m.remoteRosters = nil
	m.remoteRosterTouch = nil
	m.presenceTTL = 0
	m.presenceDone = nil
}
