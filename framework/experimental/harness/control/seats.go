package control

import (
	"sync"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// DefaultStreamSeats is the per-principal cap on long-lived control streams.
const DefaultStreamSeats = 16

// Seat is one admitted control stream; Done closes once when it is evicted.
type Seat struct {
	principal string
	Done      chan struct{}
}

// SeatDone implements stream.SeatMember.
func (s *Seat) SeatDone() chan struct{} { return s.Done }

// SeatTable tracks live control-plane stream seats per principal as a FIFO.
type SeatTable struct {
	mu    sync.Mutex
	order map[string][]*Seat
}

// NewSeatTable returns an empty seat table.
func NewSeatTable() *SeatTable {
	return &SeatTable{order: make(map[string][]*Seat)}
}

var defaultSeatTable = NewSeatTable()

// DefaultSeatTable returns the process-wide table the REST, WS and MCP HTTP transports share.
func DefaultSeatTable() *SeatTable { return defaultSeatTable }

func seatsFor(n int) int {
	if n == 0 {
		return DefaultStreamSeats
	}
	return n
}

func (t *SeatTable) spliceSeatLocked(seat *Seat) {
	stream.SpliceSeat(t.order, seat.principal, seat)
}

// AcquireSeat seats a stream for principal under max (0 default, negative unlimited); false means refused.
func (t *SeatTable) AcquireSeat(principal string, max int, overflow stream.SeatOverflowPolicy) (*Seat, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return stream.AdmitSeat(t.order, principal, seatsFor(max), overflow,
		func(p string) *Seat { return &Seat{principal: p, Done: make(chan struct{})} })
}

// ReleaseSeat removes a seat; releasing an already-evicted seat is a no-op.
func (t *SeatTable) ReleaseSeat(seat *Seat) {
	t.mu.Lock()
	t.spliceSeatLocked(seat)
	t.mu.Unlock()
}
