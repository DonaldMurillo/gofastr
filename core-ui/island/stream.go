package island

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// ssePayload is the JSON payload sent as SSE data for island updates.
type ssePayload struct {
	Island string `json:"island"`
	HTML   string `json:"html"`
}

// ServeSSE is an http.HandlerFunc that streams island updates to a client.
// It is the backward-compatible entry point (no presence); ServeSSEWithPresence
// is the presence-aware variant.
//
// The client connects via EventSource and receives island updates as SSE events.
// The client must pass a "session" query parameter to identify its session.
// Example: GET /islands/sse?session=abc123
func (m *Manager) ServeSSE(w http.ResponseWriter, r *http.Request) {
	m.ServeSSEWithPresence(w, r, PresenceIdentity{}, nil)
}

// ServeSSEWithPresence streams island updates AND registers the connection
// on one or more presence topics for the duration of the SSE stream. identity
// is the SERVER-DERIVED user identity (from the request context; never a
// client param). topics is the bounded, parsed ?presence= list. When topics
// is empty this behaves identically to ServeSSE. The presence handle is
// removed (Leave) on disconnect, including the ref-counted last-tab case:
// every ServeSSEWithPresence call gets its own handle, so closing one tab
// drops exactly that connection's contribution to the roster.
func (m *Manager) ServeSSEWithPresence(w http.ResponseWriter, r *http.Request, identity PresenceIdentity, topics []string) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session query parameter", http.StatusBadRequest)
		return
	}

	sse := stream.NewSSEWriter(w)

	// Gate the requested topics through AuthorizeTopic (nil hook = all
	// allowed) BEFORE anything is subscribed. Running the app-supplied hook
	// here, before ConnectSession, means a panicking hook cannot leak a
	// subscription (there is none yet); net/http recovers the request and
	// nothing needs teardown. An unauthorized topic never yields a
	// subscription or a roster entry. The request context carries the
	// server-derived user the hook authorizes against.
	topics = m.filterAuthorizedTopics(r.Context(), topics)

	// Subscribe BEFORE flushing headers. Flushing headers (the "connected"
	// comment below) unblocks the HTTP client, its Do() returns, so a
	// client that pushes immediately on connect would otherwise win the
	// race against this subscription and have its update silently dropped
	// by Push (which no-ops while no stream entry exists yet). Subscribing
	// first guarantees the buffered stream exists before the client can
	// observe the connection as ready. This was a timing-dependent CI flake
	// in TestServeSSE (5s timeout) on loaded runners.
	ch, cancel, err := m.ConnectSession(sessionID)
	if err != nil {
		// Reject-not-evict: a stream over the cap is refused with a clear
		// status and the streams already held are left alone. 429 is the
		// semantically correct code for a capacity limit; Retry-After keeps
		// a well-behaved client (e.g. EventSource auto-reconnect) from
		// hammering the endpoint. No SSE headers have been committed yet
		// (NewSSEWriter defers them to the first write), so this is a clean
		// error response, not a superfluous WriteHeader.
		w.Header().Set("Retry-After", "1")
		http.Error(w, err.Error(), http.StatusTooManyRequests)
		return
	}
	// cancel is deferred IMMEDIATELY: PresenceJoin fires the app-supplied
	// OnPresenceChange hook, and a panicking hook (net/http recovers it)
	// must not strand the subscription we just created. Same reasoning as
	// running filterAuthorizedTopics before ConnectSession above.
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
			// Keepalive comment: keeps intermediaries from idle-killing the
			// connection and keeps the stream writing so a dead peer is heard
			// from as a write error rather than silence.
			if err := sse.WriteComment("ping"); err != nil {
				return
			}
		}
	}
}

// ConnectSession establishes one SSE subscriber on the session's stream,
// returning this connection's private update channel, its cancel, and an
// error when the concurrent-stream caps refuse the connect (see
// WithStreamCaps). A refused connect returns a nil channel and a no-op
// cancel; the caller (ServeSSEWithPresence) surfaces a clear HTTP status.
func (m *Manager) ConnectSession(sessionID string) (<-chan IslandUpdate, func(), error) {
	return m.subscribeImpl(sessionID)
}
