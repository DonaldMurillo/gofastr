package island

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// ssePayload is the JSON payload sent as SSE data for island updates.
type ssePayload struct {
	Island string `json:"island"`
	HTML   string `json:"html"`
}

// maxSessionIDLen bounds the client-supplied ?session= value. The id is the
// m.streams map key for the stream's lifetime (default 4096 global streams),
// so an unbounded value is a RAM-pin vector from one unauthenticated client.
const maxSessionIDLen = 256

// ServeSSE is an http.HandlerFunc that streams island updates to a client.
// It is the backward-compatible entry point (no presence); ServeSSEWithPresence
// is the presence-aware variant.
//
// The client connects via EventSource and receives island updates as SSE events.
// The client must pass a "session" query parameter to identify its session.
// Example: GET /islands/sse?session=abc123
func (m *Manager) ServeSSE(w http.ResponseWriter, r *http.Request) {
	m.ServeSSEWithPresence(w, r, PresenceIdentity{}, PresenceTopics{})
}

// ServeSSEWithPresence streams island updates AND registers the connection
// on one or more presence topics for the duration of the SSE stream. identity
// is the SERVER-DERIVED user identity (from the request context; never a
// client param). topics carries the page's declared topics and the client's
// requested ones; admitTopics decides which join (see PresenceTopics). When
// both are empty this behaves identically to ServeSSE. The presence handle is
// removed (Leave) on disconnect, including the ref-counted last-tab case:
// every ServeSSEWithPresence call gets its own handle, so closing one tab
// drops exactly that connection's contribution to the roster.
func (m *Manager) ServeSSEWithPresence(w http.ResponseWriter, r *http.Request, identity PresenceIdentity, pt PresenceTopics) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonSessionMissing)
		return
	}
	// The id is pinned as the m.streams key for the stream's lifetime; an
	// unbounded value lets one client pin caps.global × len(session) bytes
	// of RAM. Bounded like every other hostile-client key (the ?presence=
	// topics cap at maxPresenceTopicLen); 256 is generous for the signed
	// session tokens the framework mints and matches the URL-carried ctx
	// bound serveChrome applies.
	if len(sessionID) > maxSessionIDLen {
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonSessionTooLong)
		return
	}

	sse := stream.NewSSEWriter(w)

	// Admit topics (declared only with no hook; every topic through
	// AuthorizeTopic with one) BEFORE anything is subscribed. Running the app-supplied hook
	// here, before ConnectSession, means a panicking hook cannot leak a
	// subscription (there is none yet); net/http recovers the request and
	// nothing needs teardown. An unauthorized topic never yields a
	// subscription or a roster entry. The request context carries the
	// server-derived user the hook authorizes against.
	topics := m.admitTopics(r.Context(), pt)

	// Subscribe BEFORE flushing headers. Flushing headers (the "connected"
	// comment below) unblocks the HTTP client, its Do() returns, so a
	// client that pushes immediately on connect would otherwise win the
	// race against this subscription and have its update silently dropped
	// by Push (which no-ops while no stream entry exists yet). Subscribing
	// first guarantees the buffered stream exists before the client can
	// observe the connection as ready. This was a timing-dependent CI flake
	// in TestServeSSE (5s timeout) on loaded runners.
	ch, cancel, err := m.ConnectSession(sessionID, m.ClientKey(r, identity))
	if err != nil {
		// Reject-not-evict: a stream over the cap is refused with a clear
		// status and the streams already held are left alone. 429 is the
		// semantically correct code for a capacity limit; Retry-After keeps
		// a well-behaved client (e.g. EventSource auto-reconnect) from
		// hammering the endpoint. No SSE headers have been committed yet
		// (NewSSEWriter defers them to the first write), so this is a clean
		// error response, not a superfluous WriteHeader.
		w.Header().Set("Retry-After", "1")
		i18n.HTTPErrorFor(w, r, http.StatusTooManyRequests, err)
		if err == ErrPeerStreamLimit {
			m.hintUntrustedProxy(r)
		}
		return
	}
	// cancel is deferred IMMEDIATELY: PresenceJoin fires the app-supplied
	// OnPresenceChange hook, and a panicking hook (net/http recovers it)
	// must not strand the subscription we just created. Same reasoning as
	// running admitTopics before ConnectSession above.
	defer cancel()
	// Presence join happens AFTER the stream exists (so the roster-change
	// callback can deliver to this connection's buffered channel) and
	// BEFORE we flush headers. Leave runs before cancel (LIFO defers) so
	// the departing connection's stream is still live while remaining
	// viewers are notified of the roster change.
	handle := m.PresenceJoin(sessionID, identity, topics)
	defer handle.Leave()

	// Flush response headers immediately so the HTTP client doesn't block
	// waiting for them.
	if err := sse.WriteComment("connected"); err != nil {
		return
	}

	// A long-lived SSE stream must satisfy three constraints at once (issue #159):
	//
	//  1. It must outlive middleware.Timeout's request deadline, a context
	//     derived via context.WithTimeout(r.Context(), d) sits on the request
	//     and would otherwise cut a live stream at d (the original bug).
	//  2. It must keep writing on an idle connection so proxies and load
	//     balancers don't idle-kill a live stream (the heartbeat).
	//  3. It must be reclaimed if the peer vanishes in a way the server cannot
	//     promptly observe, a stranded stream whose heartbeat writes keep
	//     succeeding into the kernel buffer would otherwise hold a socket
	//     forever, exhausting the browser's per-origin connection pool (the
	//     #158 regression).
	//
	// This is NOT done by clearing the connection's read/write deadlines (the
	// reverted 217e8d06 did, which stranded streams by defeating net/http's
	// close-notify). Instead:
	//
	//   - the request-context deadline is distinguished from a real disconnect:
	//     DeadlineExceeded is middleware.Timeout firing on a still-connected
	//     client (ignored, let the bound decide lifetime); context.Canceled is
	//     a genuine peer disconnect (unwind immediately);
	//   - a heartbeat ticker writes a keepalive comment so a live stream is
	//     never idle and a half-closed peer eventually surfaces as a write
	//     error;
	//   - a bounded-lifetime timer closes any stream after a fixed duration
	//     regardless of write success, the safety net for a stranded stream.
	//     The bound exceeds the heartbeat; EventSource reconnects automatically.
	reqCtx := r.Context()
	streamCtx, streamCancel := context.WithCancel(context.Background())
	defer streamCancel()
	go func() {
		<-reqCtx.Done()
		// DeadlineExceeded == the request timeout fired on a live client: the
		// original bug. Leave the stream running; the bound owns its lifetime.
		// context.Canceled == the peer actually went away: reclaim promptly.
		if reqCtx.Err() == context.Canceled {
			streamCancel()
		}
	}()

	// held re-establishes the viewer and re-runs the topic gate against the
	// refreshed context. AuthorizeTopic answered for the viewer at connect;
	// one whose access to a joined topic was removed since must leave that
	// roster and stop receiving its pushes, so the stream closes and the
	// reconnect meets the gate again. With no check and no hook installed
	// this is a no-op.
	held := func() bool {
		fresh, ok := handler.RecheckPrincipal(reqCtx)
		if !ok {
			return false
		}
		return len(m.filterAuthorizedTopics(fresh, topics)) == len(topics)
	}

	heartbeat := time.NewTicker(m.sseHeartbeat)
	defer heartbeat.Stop()
	bound := time.NewTimer(m.sseStreamBound)
	defer bound.Stop()

	for {
		select {
		case <-streamCtx.Done():
			return
		case <-bound.C:
			// Bounded lifetime: a stranded stream is reclaimed even when its
			// heartbeat writes keep succeeding. EventSource reconnects on a
			// live stream; a dead peer's socket is simply released.
			return
		case update := <-ch:
			// The updates pushed to this stream are rendered for its viewer.
			// Re-establish that viewer before each one (and on the heartbeat
			// below): a session revoked mid-stream, or a topic it may no
			// longer join, closes the stream at the next push rather than at
			// the stream bound.
			if !held() {
				return
			}
			payload := ssePayload{
				Island: update.IslandID,
				HTML:   update.HTML,
			}
			data, err := json.Marshal(payload)
			if err != nil {
				sse.WriteError("json marshal error")
				continue
			}
			if err := sse.WriteEvent("island", string(data)); err != nil {
				return
			}
		case <-heartbeat.C:
			if !held() {
				return
			}
			// Keepalive comment: keeps intermediaries from idle-killing the
			// connection and keeps the stream writing so a dead peer is heard
			// from as a write error rather than silence.
			if err := sse.WriteComment("ping"); err != nil {
				return
			}
		}
	}
}

// ConnectSession establishes one SSE subscriber on the session's stream for
// the client peer (its ClientKey), returning this connection's private update
// channel, its cancel, and an error when the concurrent-stream caps refuse the
// connect (see WithStreamCaps and WithPeerStreamCap). A refused connect returns
// a nil channel and a no-op cancel; the caller (ServeSSEWithPresence) surfaces
// a clear HTTP status. An empty peer is counted by no per-client cap.
func (m *Manager) ConnectSession(sessionID, peer string) (<-chan IslandUpdate, func(), error) {
	return m.subscribeImpl(sessionID, peer)
}

// hintUntrustedProxy warns once when the per-client cap refuses an anonymous
// stream from a private or loopback peer while no proxy is trusted: that is
// the shape of a reverse proxy nobody listed, where every visitor counts as
// the proxy's one address and shares one budget.
func (m *Manager) hintUntrustedProxy(r *http.Request) {
	ip := clientip.ParseHop(r.RemoteAddr)
	if !m.proxies.Empty() || ip == nil || !(ip.IsLoopback() || ip.IsPrivate()) {
		return
	}
	m.proxyHint.Do(func() {
		slog.Default().Warn("island: the per-client SSE stream cap refused a stream from a private address; behind a reverse proxy, list it with uihost.WithTrustedProxies or every visitor shares its budget",
			"peer", ip.String(), "cap", m.caps.perPeer)
	})
}

// ClientKey is the client a stream request counts under for the per-client
// cap: the signed-in user when identity names one, otherwise the caller's
// address. The address is the TCP peer, or the X-Forwarded-For hop a
// WithTrustedProxies proxy vouches for when the peer is one of them. An IPv6
// address counts by its /64, which one host commonly owns whole, so rotating
// source addresses inside it buys no fresh budget.
func (m *Manager) ClientKey(r *http.Request, identity PresenceIdentity) string {
	if identity.UserID != "" {
		return "user:" + identity.UserID
	}
	ip := clientip.ParseHop(r.RemoteAddr)
	if ip != nil && m.proxies.Contains(ip) {
		if fwd, ok := clientip.Forwarded(r.Header, m.proxies); ok {
			ip = fwd
		}
	}
	switch {
	case ip == nil:
		return "peer:" + r.RemoteAddr
	case ip.To4() != nil:
		return "ip:" + ip.String() // a v4-mapped v6 address prints as v4
	default:
		return "ip:" + ip.Mask(net.CIDRMask(64, 128)).String()
	}
}
