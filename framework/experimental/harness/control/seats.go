package control

import (
	"sync"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// DefaultStreamSeats is the per-principal cap the control-plane stream
// surfaces (REST SSE, websocket, MCP streamable-HTTP GET) enforce: the
// same number core/stream's SSE broker, core/mcp's notification
// stream, and framework/crud's event stream cap at, so the seat
// policies in the tree read as one number. Sixteen covers multi-tab /
// multi-device use of one credential while bounding what a single
// token can hold resident (each stream parks a handler goroutine, a
// bus subscription, and a revocation ticker; a websocket adds a run
// loop, an event pump, and a revocation watcher for the life of the
// TCP connection).
const DefaultStreamSeats = 16

// Seat is one admitted long-lived control stream, held from connect
// until the transport's loop exits. Done is closed exactly once, by
// whichever SeatTable path removes the seat from its principal's FIFO
// (an EvictOldest admission or the stream's own departure through
// Release); the stream's loop treats it as a disconnect.
type Seat struct {
	principal string
	Done      chan struct{}
}

// SeatDone implements stream.SeatMember so the shared
// admission helper (stream.AdmitSeat) can count, evict, and close the
// seat.
func (s *Seat) SeatDone() chan struct{} { return s.Done }

// SeatTable tracks live control-plane stream seats per principal as a
// FIFO, the shape a SeatOverflowEvictOldest admission pops from. The
// REST, WS, and MCP HTTP transports share ONE table through
// DefaultSeatTable: they are one control plane, and the cap counts per
// credential, not per transport.
type SeatTable struct {
	mu    sync.Mutex
	order map[string][]*Seat
}

// NewSeatTable returns an empty seat table.
func NewSeatTable() *SeatTable {
	return &SeatTable{order: make(map[string][]*Seat)}
}

// defaultSeatTable is the process-wide table the control transports
// share when none is wired into their struct: a hand-built Server or
// Handler still gets the cap, and the cap is per token across
// transports.
var defaultSeatTable = NewSeatTable()

// DefaultSeatTable returns the process-wide control-plane seat table.
func DefaultSeatTable() *SeatTable { return defaultSeatTable }

// seatsFor resolves the cap: 0 = DefaultStreamSeats, negative =
// unlimited (deployments that bound streams elsewhere).
func seatsFor(n int) int {
	if n == 0 {
		return DefaultStreamSeats
	}
	return n
}

// spliceSeatLocked removes seat from its principal's FIFO; the caller
// holds t.mu. A no-op when the seat is already gone (released after an
// eviction, or evicted after a release), so a seat is removed exactly
// once and the FIFO never retains a departed stream. The splice itself
// is the generic stream.SpliceSeat, shared with core/stream's broker,
// core/mcp's server, and framework/crud's stream-seat registry.
func (t *SeatTable) spliceSeatLocked(seat *Seat) {
	stream.SpliceSeat(t.order, seat.principal, seat)
}

// AcquireSeat seats one stream for principal under the given cap and
// overflow policy, returning the seat. The bool is false when the
// principal is at the cap and the policy is Refuse: the caller answers
// 429 and holds nothing. Under EvictOldest the principal's oldest seat
// is closed and removed in the same critical section, and the new
// stream is seated. The body is the shared stream.AdmitSeat, which the
// control plane formerly duplicated.
func (t *SeatTable) AcquireSeat(principal string, max int, overflow stream.SeatOverflowPolicy) (*Seat, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return stream.AdmitSeat(t.order, principal, seatsFor(max), overflow,
		func(p string) *Seat { return &Seat{principal: p, Done: make(chan struct{})} })
}

// ReleaseSeat departs a seat: the transport's loop exited, the stream
// broke, or a re-auth refusal closed it. Splicing is idempotent, so
// releasing a seat an EvictOldest admission already removed is a
// no-op.
func (t *SeatTable) ReleaseSeat(seat *Seat) {
	t.mu.Lock()
	t.spliceSeatLocked(seat)
	t.mu.Unlock()
}
