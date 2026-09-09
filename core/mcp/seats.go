package mcp

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

// defaultSSESeatCap is the per-caller cap on concurrent SSE notification
// streams when SetSSESeatCap has not been called (or was called with 0).
// Sixteen covers every legitimate MCP client shape (a client holds ONE
// notification stream; the surplus is multi-client hosts), while binding
// the per-connection cost — one handler goroutine plus a
// sseSubBufferSize buffered channel — that an unauthenticated peer
// could otherwise grow without end.
const defaultSSESeatCap = 16

// SeatOverflowPolicy selects what a caller at the SSE seat cap does with
// its next stream. It is core/stream's policy type under this package's
// name: the two were verbatim copies, and the SSE bus, the crud event
// streams, and this server all seat by the same rule.
type SeatOverflowPolicy = stream.SeatOverflowPolicy

const (
	// SeatOverflowRefuse answers the connection with 429 and holds no
	// seat. The default: a caller at the cap learns it is at the cap.
	SeatOverflowRefuse = stream.SeatOverflowRefuse
	// SeatOverflowEvictOldest closes that caller's oldest stream and
	// seats the new one, the reconnect-friendly policy for hosts whose
	// clients churn streams.
	SeatOverflowEvictOldest = stream.SeatOverflowEvictOldest
)

// SetSSESeatCap bounds how many concurrent SSE notification streams
// (ServeSSE's GET half) one caller may hold. The caller is the resolved
// user when middleware put one in the request context (its GetID), else
// the TCP peer. n == 0 restores the default (16); n < 0 lifts the cap
// for deployments that bound seats elsewhere — every held stream still
// costs a goroutine and a buffered channel, so lifting it is a deliberate
// decision, not the zero-value accident.
func (s *Server) SetSSESeatCap(n int) {
	s.sseMu.Lock()
	s.sseSeatCap = n
	s.sseMu.Unlock()
}

// SetSSESeatOverflow selects the seat overflow policy; see
// SeatOverflowPolicy. The zero value (SeatOverflowRefuse) is the
// default.
func (s *Server) SetSSESeatOverflow(p SeatOverflowPolicy) {
	s.sseMu.Lock()
	s.sseSeatOverflow = p
	s.sseMu.Unlock()
}

// sseSeatKey derives the per-caller seat key: the authenticated user's
// id when upstream middleware resolved one into the request context,
// else the TCP peer. The prefixes keep the two namespaces from
// colliding (a user id never aliases a RemoteAddr and back).
func sseSeatKey(ctx context.Context, r *http.Request) string {
	if u, ok := handler.GetUser(ctx); ok {
		if id, ok := u.(interface{ GetID() string }); ok && id.GetID() != "" {
			return "user:" + id.GetID()
		}
	}
	if r != nil && r.RemoteAddr != "" {
		return "peer:" + r.RemoteAddr
	}
	return "anon"
}

// seatCapLocked resolves the configured cap; the caller holds sseMu.
// Zero means the default; negative means unlimited.
func (s *Server) seatCapLocked() int {
	if s.sseSeatCap == 0 {
		return defaultSSESeatCap
	}
	return s.sseSeatCap
}

// spliceSeatLocked removes sub from its caller's seat FIFO without
// touching sseSubs or the channel; the caller holds sseMu. Used by
// removeSSESubscriber (voluntary departure), the admission eviction
// path, and notifySubscribers' backpressure drop, so the FIFO never
// retains a departed subscriber and a seat is freed exactly once.
// The splice itself is stream.SpliceSeat, shared with core/stream's
// broker and framework/crud's stream-seat registry.
func (s *Server) spliceSeatLocked(sub *sseSubscriber) {
	stream.SpliceSeat(s.sseSeatOrder, sub.seatKey, sub)
}
