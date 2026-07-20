package island

import (
	"encoding/json"
	"net/http"

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
	// here — before ConnectSession — means a panicking hook cannot leak a
	// subscription (there is none yet); net/http recovers the request and
	// nothing needs teardown. An unauthorized topic never yields a
	// subscription or a roster entry. The request context carries the
	// server-derived user the hook authorizes against.
	topics = m.filterAuthorizedTopics(r.Context(), topics)

	// Subscribe BEFORE flushing headers. Flushing headers (the "connected"
	// comment below) unblocks the HTTP client — its Do() returns — so a
	// client that pushes immediately on connect would otherwise win the
	// race against this subscription and have its update silently dropped
	// by Push (which no-ops while no stream entry exists yet). Subscribing
	// first guarantees the buffered stream exists before the client can
	// observe the connection as ready. This was a timing-dependent CI flake
	// in TestServeSSE (5s timeout) on loaded runners.
	ch, cancel := m.ConnectSession(sessionID)
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

	// Respect client context cancellation — the sole exit signal; this
	// connection's channel is private, so no sibling can tear it down.
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
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
		}
	}
}

// ConnectSession establishes one SSE subscriber on the session's stream,
// returning this connection's private update channel and its cancel.
func (m *Manager) ConnectSession(sessionID string) (<-chan IslandUpdate, func()) {
	return m.Subscribe(sessionID)
}
