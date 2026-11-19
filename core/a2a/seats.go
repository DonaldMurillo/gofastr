package a2a

import (
	"sync"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

type streamSeat struct {
	owner string
	done  chan struct{}
}

func (s *streamSeat) SeatDone() chan struct{} { return s.done }

type streamSeatRegistry struct {
	mu    sync.Mutex
	order map[string][]*streamSeat
}

func newStreamSeatRegistry() *streamSeatRegistry {
	return &streamSeatRegistry{order: make(map[string][]*streamSeat)}
}

func (r *streamSeatRegistry) admitSeat(owner string, cap int, overflow stream.SeatOverflowPolicy) (*streamSeat, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return stream.AdmitSeat(r.order, owner, cap, overflow, func(p string) *streamSeat {
		return &streamSeat{owner: p, done: make(chan struct{})}
	})
}

func (r *streamSeatRegistry) releaseSeat(seat *streamSeat) {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-seat.done:
	default:
		close(seat.done)
	}
	stream.SpliceSeat(r.order, seat.owner, seat)
}
