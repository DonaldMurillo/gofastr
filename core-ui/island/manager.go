package island

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
)

// IslandUpdate represents an update to push to a client.
type IslandUpdate struct {
	IslandID string
	HTML     string
}

// streamEntry holds a session's live subscribers. Each subscriber (one
// SSE connection — e.g. each browser tab sharing the session cookie)
// gets its OWN buffered channel, and deliver broadcasts to all of them.
// The previous single-shared-channel design made same-session tabs
// COMPETE for updates (one channel, first receiver wins); per-subscriber
// channels are what "every subscriber receives the update" requires.
// Data channels are never closed (senders can't panic); cancel removes
// the channel from the map and the GC reclaims it.
type streamEntry struct {
	subs map[uint64]chan IslandUpdate // subscriber id → its buffered channel
}

// streamCaps bounds how many concurrent SSE streams one replica tolerates.
// An anonymous same-origin caller can mint a session (POST /__gofastr/session)
// and then open an unbounded number of EventSource connections against it; each
// Subscribe allocates a goroutine-sized buffered channel (64 slots) that lives
// until the client disconnects. Without a ceiling a single client — or a
// reconnect-loop bug — holds them forever. The policy is REJECT-not-evict (the
// v0.39 SSE review): a stream over the cap is refused with a clear status, and
// a new stream NEVER silently drops an existing one. A field value of 0 means
// "unlimited"; the defaults are applied by NewManager.
type streamCaps struct {
	perSession int // max streams sharing one session id (one per tab/connection)
	global     int // max streams across all sessions on this replica
}

// Default concurrent-SSE-stream ceilings. perSession is generous for real
// multi-tab use but small enough that a runaway client can't pin dozens of
// goroutines to one session; global protects one replica's RAM. Override per
// manager via WithStreamCaps.
const (
	DefaultStreamsPerSession = 16
	DefaultGlobalStreams     = 4096
)

// ErrSessionStreamLimit is returned when a session already holds its per-session
// cap of concurrent SSE streams. The connect is refused, not evicted.
var ErrSessionStreamLimit = errors.New("island: per-session SSE stream limit reached")

// ErrGlobalStreamLimit is returned when the replica already holds its global cap
// of concurrent SSE streams. The connect is refused, not evicted.
var ErrGlobalStreamLimit = errors.New("island: global SSE stream limit reached")

// ManagerOption configures a Manager at construction (NewManager).
type ManagerOption func(*Manager)

// WithStreamCaps overrides the concurrent-SSE-stream ceilings. perSession bounds
// how many streams a single session id may hold open (one per browser tab /
// EventSource connection); global bounds the total across all sessions on this
// replica. A value of 0 means "unlimited" for that dimension.
func WithStreamCaps(perSession, global int) ManagerOption {
	return func(m *Manager) {
		m.caps.perSession = perSession
		m.caps.global = global
	}
}

// Manager tracks active islands across all client sessions.
type Manager struct {
	mu      sync.RWMutex
	streams map[string]*streamEntry // sessionID → update stream

	// caps bounds concurrent SSE streams (per-session + global). Defaults
	// from DefaultStreamsPerSession / DefaultGlobalStreams; override with
	// WithStreamCaps. globalSubs tracks the live total across all sessions
	// (guarded by mu) so the global cap is a cheap integer compare rather
	// than a map walk on every connect.
	caps       streamCaps
	globalSubs int

	// ── Presence ──
	// presenceConns maps a unique connection id to its presence
	// registration (identity + topics). The LOCAL roster is derived from
	// this live set at read time (dedup by UserID), so there is no manual
	// ref-count to drift. nextPresenceID is atomically incremented and
	// is safe to use without holding mu. OnPresenceChange, when set, is
	// fired outside the lock after a local OR remote roster change
	// mutates a topic's merged roster. See presence.go (local) and
	// presence_fanout.go (cross-replica).
	presenceConns  map[uint64]*presenceConn
	nextPresenceID uint64
	nextSubID      uint64 // stream-subscriber id source; guarded by mu

	// OnPresenceChange, when set, is fired outside the lock after a local OR
	// remote roster change mutates a topic's merged roster. See presence.go
	// (local) and presence_fanout.go (cross-replica). Install with
	// SetOnPresenceChange BEFORE serving traffic; the atomic.Pointer keeps the
	// install race-free against the per-roster-change read.
	onPresenceChange atomic.Pointer[func(string)]

	// AuthorizeTopic, when set, gates which presence topics a connection may
	// join. It is called once per requested topic at SSE-connect time with the
	// request context (carrying the server-derived authenticated user);
	// returning false drops that topic BEFORE any subscription or roster
	// emission, so an unauthorized viewer never sees the roster (which can
	// contain emails) and never receives join/leave events. A nil hook
	// authorizes every topic — presence is public by default (opt-in gating),
	// so existing apps are unaffected. Install with SetAuthorizeTopic BEFORE
	// serving traffic; the atomic.Pointer keeps the install race-free against
	// the SSE-connect read.
	authorizeTopic atomic.Pointer[func(context.Context, string) bool]

	// dropped counts island updates discarded because a client's SSE buffer
	// was full (slow/stalled consumer). Exposed via DroppedUpdates so the
	// otherwise-silent loss is observable (wire it to a metric/health check).
	dropped atomic.Int64

	// fanout, when attached via SetFanout, mirrors PushUpdate updates
	// to other replicas and re-delivers theirs locally. nodeID is the
	// originator stamp used to drop own-node echoes. fanoutSend is the
	// non-blocking enqueue into the publish queue (fanout.PublishQueue) —
	// PushUpdate runs on HTTP request goroutines and must never wait on
	// the backend's network/DB round-trip. All guarded by mu.
	fanout     fanout.Fanout
	nodeID     string
	fanoutSend func([]byte)

	// ── Presence fanout (cross-replica; presence_fanout.go) ──
	// The SAME transport as `fanout` above, used for a DEDICATED presence
	// lane on topic presenceFanoutTopic ("gofastr.presence") — parallel to
	// the island-invalidation lane, never sharing its payload shape.
	// presenceSend is the non-blocking enqueue into the presence publish
	// queue. remoteRosters maps topic → replicaID → entry (with TTL); nil
	// when no fanout is attached (PresenceRoster then returns local-only).
	// presenceHeartbeat/presenceTTL are the convergence intervals (defaults
	// set in SetFanout; test-tunable via reconfigurePresence). presenceDone
	// closes to stop the heartbeat goroutine; presenceWG tracks it so stop
	// waits and no goroutine leaks. All guarded by mu except presenceWG.
	presenceSend      func([]byte)
	remoteRosters     map[string]map[string]remoteRosterEntry
	presenceHeartbeat time.Duration
	presenceTTL       time.Duration
	presenceDone      chan struct{}
	presenceWG        sync.WaitGroup
}

// DroppedUpdates returns the cumulative number of island updates dropped
// because a client's SSE channel was full. A steadily climbing value means
// consumers can't keep up (undersized buffer, stalled browsers).
func (m *Manager) DroppedUpdates() int64 { return m.dropped.Load() }

// SetOnPresenceChange installs the roster-change callback. Install BEFORE
// serving traffic (the typical wire-time set). Stored in an atomic.Pointer so
// the install races nothing with the per-change read — a plain field here was a
// data race under -race when an app installed it concurrently with a roster
// change firing. Replaces the old direct field assignment.
func (m *Manager) SetOnPresenceChange(fn func(topic string)) {
	if fn == nil {
		m.onPresenceChange.Store(nil)
		return
	}
	m.onPresenceChange.Store(&fn)
}

// SetAuthorizeTopic installs the topic-authorization gate. Install BEFORE
// serving traffic. Stored in an atomic.Pointer for the same race-safety reason
// as SetOnPresenceChange. Replaces the old direct field assignment.
func (m *Manager) SetAuthorizeTopic(fn func(ctx context.Context, topic string) bool) {
	if fn == nil {
		m.authorizeTopic.Store(nil)
		return
	}
	m.authorizeTopic.Store(&fn)
}

// presenceCallback returns the installed OnPresenceChange hook or nil. The
// atomic load races nothing with SetOnPresenceChange.
func (m *Manager) presenceCallback() func(string) {
	if p := m.onPresenceChange.Load(); p != nil {
		return *p
	}
	return nil
}

// topicAuthorizer returns the installed AuthorizeTopic hook or nil. The atomic
// load races nothing with SetAuthorizeTopic.
func (m *Manager) topicAuthorizer() func(context.Context, string) bool {
	if p := m.authorizeTopic.Load(); p != nil {
		return *p
	}
	return nil
}

// NewManager creates a new island manager. Options override the default
// concurrent-SSE-stream caps (see WithStreamCaps); the defaults already bound
// one replica, so most callers pass nothing.
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		streams: make(map[string]*streamEntry),
		caps: streamCaps{
			perSession: DefaultStreamsPerSession,
			global:     DefaultGlobalStreams,
		},
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Subscribe registers a new subscriber on the session's stream and
// returns its OWN buffered update channel plus a cancel func that
// removes exactly this subscription (idempotent). Every subscriber —
// each tab sharing the session cookie — receives every update; the
// session's entry is deleted when its last subscriber cancels.
func (m *Manager) Subscribe(sessionID string) (<-chan IslandUpdate, func()) {
	ch, cancel, _ := m.subscribeImpl(sessionID)
	return ch, cancel
}

// subscribeImpl is the single cap-enforcing chokepoint. It checks BOTH ceilings
// AND adds the subscriber atomically under mu, so two concurrent connects cannot
// both pass a ceiling and then both land. A refused connect returns a nil channel,
// a no-op cancel, and a sentinel error (ErrSessionStreamLimit / ErrGlobalStreamLimit);
// existing streams are untouched (reject-not-evict). The global counter is maintained
// here and in the returned cancel so it stays consistent regardless of which entry
// point (Subscribe, ConnectSession) produced the subscription.
func (m *Manager) subscribeImpl(sessionID string) (<-chan IslandUpdate, func(), error) {
	m.mu.Lock()
	entry, ok := m.streams[sessionID]
	if m.caps.perSession > 0 && ok && len(entry.subs) >= m.caps.perSession {
		m.mu.Unlock()
		return nil, func() {}, ErrSessionStreamLimit
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

// islandFanoutTopic is the single fanout channel the island managers across
// replicas publish on and subscribe to.
const islandFanoutTopic = "gofastr.islands"

// islandFanoutMsg is the wire shape for a fanned-out update. sessionID must
// travel inside the payload because the receiving replica delivers by
// looking up its own streams[sessionID] — it does not know the session
// otherwise.
type islandFanoutMsg struct {
	SessionID string `json:"s"`
	IslandID  string `json:"i"`
	HTML      string `json:"h"`
}

// deliver sends update to the local stream for sessionID if present, using
// the same non-blocking send + dropped-counter semantics as before the
// fanout seam. Shared by PushUpdate and the fanout receive path so a
// remote-delivered update is indistinguishable from a local one at the
// channel level.
func (m *Manager) deliver(update IslandUpdate, sessionID string) {
	// Copy the subscriber channels under the read lock, send outside it.
	// A concurrent cancel between copy and send just means a buffered
	// send nobody drains — channels are never closed, so no panic.
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
			// Drop for THIS subscriber if its buffer is full — a slow
			// tab must not stall its siblings.
			m.dropped.Add(1)
		}
	}
}

// publishFanout mirrors update to other replicas via the attached fanout, if
// any. Best-effort: a dropped real-time message is acceptable (the durable
// lane is the outbox's job, not the island lane). No-op when no fanout is
// attached. The enqueue never blocks — callers are HTTP request goroutines
// and a stalled backend must not stall them (see fanout.PublishQueue).
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

// SetFanout attaches a fanout so PushUpdate updates cross replicas and
// updates originating on other replicas are re-delivered to the local
// session stream (topic "gofastr.islands"). It ALSO wires the cross-replica
// PRESENCE lane (topic "gofastr.presence") over the same transport so a
// topic's merged roster reflects every replica's connections, not just this
// one — see presence_fanout.go. Own-node messages are dropped on both lanes
// so updates/announcements are never echoed back, and received messages are
// NEVER re-publishing (no loop). Delivery is lossy best-effort; presence
// reconverges via periodic full-roster heartbeats.
//
// Island delivery-where-connected is fixed here; the manager retains no
// island objects (callers render from reconstructable state and PushUpdate
// transports the HTML), so any replica serves any RPC — no sticky routing.
// Presence state is fully aggregated across replicas.
//
// The returned stop detaches BOTH lanes (cancels subscriptions, stops the
// publish queues, stops the presence heartbeat goroutine, publishes a
// graceful-leave so peers drop this replica promptly, and clears the
// bridge); safe to call multiple times. Returns an error if a fanout is
// already attached or f is nil.
func (m *Manager) SetFanout(f fanout.Fanout) (stop func(), err error) {
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
	// Presence lane state.
	m.presenceSend = presenceSend
	m.remoteRosters = make(map[string]map[string]remoteRosterEntry)
	m.presenceHeartbeat = defaultPresenceHeartbeat
	m.presenceTTL = defaultPresenceTTL
	m.presenceDone = make(chan struct{})
	m.mu.Unlock()

	// Island lane: re-deliver remote updates to local session streams.
	islandCancel, subErr := f.Subscribe(islandFanoutTopic, func(raw []byte) {
		origin, body, uerr := fanout.Unwrap(raw)
		if uerr != nil {
			return
		}
		if origin == nodeID {
			return // own-node: drop to avoid echo
		}
		var msg islandFanoutMsg
		if jerr := json.Unmarshal(body, &msg); jerr != nil {
			return
		}
		// Deliver locally only; NEVER re-publish on receive.
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

	// Presence lane: merge remote announcements into the local roster table.
	presenceCancel, presenceSubErr := f.Subscribe(presenceFanoutTopic, func(raw []byte) {
		origin, body, uerr := fanout.Unwrap(raw)
		if uerr != nil {
			return
		}
		if origin == nodeID {
			return // own-node: drop; this replica's rosters are already local
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

	// Start the presence heartbeat goroutine (bound to the presenceDone
	// channel we just created) and immediately announce this replica's
	// current local rosters so peers converge without waiting for the first
	// beat (covers joins that happened before SetFanout attached).
	m.mu.RLock()
	presenceDone := m.presenceDone
	m.mu.RUnlock()
	m.presenceWG.Add(1)
	go m.presenceHeartbeatLoop(presenceDone)
	m.broadcastAllLocalTopics()

	var once sync.Once
	return func() {
		once.Do(func() {
			// Graceful leave: tell peers to drop this replica now (TTL is
			// only the crash fallback). No-op if the heartbeat was halted.
			m.gracefulLeaveLocalTopics(f)
			// Stop the heartbeat goroutine.
			m.mu.Lock()
			done := m.presenceDone
			m.mu.Unlock()
			if done != nil {
				close(done)
			}
			m.presenceWG.Wait()
			// Detach both lanes' subscriptions + publish queues.
			islandCancel()
			presenceCancel()
			islandStopQueue()
			presenceStopQueue()
			// Clear all fanout + presence state.
			m.mu.Lock()
			m.fanout = nil
			m.nodeID = ""
			m.fanoutSend = nil
			m.clearPresenceFanoutLocked()
			m.mu.Unlock()
		})
	}, nil
}

// clearPresenceFanoutLocked resets the cross-replica presence fields. Caller
// holds mu. Used by SetFanout's rollback and stop paths so neither leaks
// state nor a goroutine into a "fresh" single-replica manager.
func (m *Manager) clearPresenceFanoutLocked() {
	m.presenceSend = nil
	m.remoteRosters = nil
	m.presenceHeartbeat = 0
	m.presenceTTL = 0
	m.presenceDone = nil
}
