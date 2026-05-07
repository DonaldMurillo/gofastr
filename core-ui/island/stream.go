package island

import (
	"encoding/json"
	"net/http"

	"github.com/gofastr/gofastr/core/stream"
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

	// Send a comment to flush response headers immediately,
	// so the HTTP client doesn't block waiting for headers.
	if err := sse.WriteComment("connected"); err != nil {
		return
	}

	ch := m.ConnectSession(sessionID)
	defer m.Unsubscribe(sessionID)

	// Respect client context cancellation.
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-ch:
			if !ok {
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
		}
	}
}

// ConnectSession establishes a session for SSE streaming.
// It subscribes to updates and returns the update channel.
func (m *Manager) ConnectSession(sessionID string) <-chan IslandUpdate {
	return m.Subscribe(sessionID)
}
