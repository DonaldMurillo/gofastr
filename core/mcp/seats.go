package mcp

import (
	"context"
	"net"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
)

const defaultSSESeatCap = 16

// SeatOverflowPolicy is what a caller at the SSE seat cap gets on its next stream; it is core/stream's type.
type SeatOverflowPolicy = stream.SeatOverflowPolicy

const (
	// SeatOverflowRefuse answers the new stream 429; the default.
	SeatOverflowRefuse = stream.SeatOverflowRefuse
	// SeatOverflowEvictOldest closes the caller's oldest stream and admits the new one.
	SeatOverflowEvictOldest = stream.SeatOverflowEvictOldest
)

// SetSSESeatCap bounds one caller's concurrent SSE notification streams; 0 restores the default of 16 and a negative n lifts the cap.
func (s *Server) SetSSESeatCap(n int) {
	s.sseMu.Lock()
	s.sseSeatCap = n
	s.sseMu.Unlock()
}

// SetSSEStreamLimit bounds the SSE notification streams of all callers together; 0 restores the default of 1024 and a negative n lifts it.
func (s *Server) SetSSEStreamLimit(n int) {
	s.sseMu.Lock()
	s.sseStreamLimit = n
	s.sseMu.Unlock()
}

// SetSSESeatOverflow sets the seat overflow policy; the zero value is SeatOverflowRefuse.
func (s *Server) SetSSESeatOverflow(p SeatOverflowPolicy) {
	s.sseMu.Lock()
	s.sseSeatOverflow = p
	s.sseMu.Unlock()
}

func sseSeatKey(ctx context.Context, r *http.Request) string {
	if u, ok := handler.GetUser(ctx); ok {
		if id, ok := u.(interface{ GetID() string }); ok && id.GetID() != "" {
			return "user:" + id.GetID()
		}
	}
	if r != nil && r.RemoteAddr != "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return "peer:" + host
		}
		return "peer:" + r.RemoteAddr
	}
	return "anon"
}

func (s *Server) seatCapLocked() int {
	if s.sseSeatCap == 0 {
		return defaultSSESeatCap
	}
	return s.sseSeatCap
}

func (s *Server) spliceSeatLocked(sub *sseSubscriber) {
	stream.SpliceSeat(s.sseSeatOrder, sub.seatKey, sub)
}

func (s *Server) streamLimitLocked() int {
	if s.sseStreamLimit == 0 {
		return defaultSSEStreamLimit
	}
	return s.sseStreamLimit
}
