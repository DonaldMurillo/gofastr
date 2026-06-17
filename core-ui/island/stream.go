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
// It uses the existing core/stream SSE infrastructure.
// The client connects via EventSource and receives island updates as SSE events.
//
// The client must pass a "session" query parameter to identify its session.
// Example: GET /islands/sse?session=abc123
func (m *Manager) ServeSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	if sessionID == "" {
		http.Error(w, "missing session query parameter", http.StatusBadRequest)
		return
	}

	sse := stream.NewSSEWriter(w)

	// Subscribe BEFORE flushing headers. Flushing headers (the "connected"
	// comment below) unblocks the HTTP client — its Do() returns — so a
	// client that pushes immediately on connect would otherwise win the
	// race against this subscription and have its update silently dropped
	// by Push (which no-ops while no stream entry exists yet). Subscribing
	// first guarantees the buffered stream exists before the client can
	// observe the connection as ready. This was a timing-dependent CI flake
	// in TestServeSSE (5s timeout) on loaded runners.
	ch := m.ConnectSession(sessionID)
	defer m.Unsubscribe(sessionID)

	// Get the done channel so we can detect unsubscribe.
	m.mu.RLock()
	entry, hasEntry := m.streams[sessionID]
	m.mu.RUnlock()
	if !hasEntry {
		return
	}

	// Flush response headers immediately so the HTTP client doesn't block
	// waiting for them.
	if err := sse.WriteComment("connected"); err != nil {
		return
	}

	// Respect client context cancellation.
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-entry.done:
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

// ConnectSession establishes a session for SSE streaming.
// It subscribes to updates and returns the update channel.
func (m *Manager) ConnectSession(sessionID string) <-chan IslandUpdate {
	return m.Subscribe(sessionID)
}
