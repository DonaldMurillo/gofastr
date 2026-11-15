package crud

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

const defaultEventStreamSeats = 16

func eventStreamSeatsFor(n int) int {
	if n == 0 {
		return defaultEventStreamSeats
	}
	return n
}

// streamSeat is one admitted EventStream connection; done is closed when
// an EvictOldest admission takes its seat.
type streamSeat struct {
	principal string
	done      chan struct{}

	cutMu    sync.Mutex
	cut      func()
	returned bool
}

func (s *streamSeat) interruptWrite() {
	s.cutMu.Lock()
	defer s.cutMu.Unlock()
	if !s.returned && s.cut != nil {
		s.cut()
	}
}

func (s *streamSeat) markReturned() {
	s.cutMu.Lock()
	s.returned = true
	s.cutMu.Unlock()
}

// SeatDone implements stream.SeatMember.
func (s *streamSeat) SeatDone() chan struct{} { return s.done }

// streamSeatRegistry holds each principal's live EventStream seats, oldest
// first.
type streamSeatRegistry struct {
	mu    sync.Mutex
	order map[string][]*streamSeat
}

func newStreamSeatRegistry() *streamSeatRegistry {
	return &streamSeatRegistry{order: make(map[string][]*streamSeat)}
}

// spliceSeatLocked removes seat if still present; the caller holds r.mu.
func (r *streamSeatRegistry) spliceSeatLocked(seat *streamSeat) {
	stream.SpliceSeat(r.order, seat.principal, seat)
}

// admit seats one stream for principal, cut being its write interrupt, and
// reports false when the principal is at cap under the Refuse policy.
func (r *streamSeatRegistry) admit(principal string, cap int, overflow stream.SeatOverflowPolicy, cut func()) (*streamSeat, bool) {
	r.mu.Lock()
	var oldest *streamSeat
	if q := r.order[principal]; len(q) > 0 {
		oldest = q[0]
	}
	seat, ok := stream.AdmitSeat(r.order, principal, eventStreamSeatsFor(cap), overflow,
		func(p string) *streamSeat { return &streamSeat{principal: p, done: make(chan struct{}), cut: cut} })
	r.mu.Unlock()
	if oldest != nil {
		select {
		case <-oldest.done:
			oldest.interruptWrite()
		default:
		}
	}
	return seat, ok
}

func (r *streamSeatRegistry) release(seat *streamSeat) {
	seat.markReturned()
	r.mu.Lock()
	r.spliceSeatLocked(seat)
	r.mu.Unlock()
}

func (r *streamSeatRegistry) liveSeats() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, q := range r.order {
		n += len(q)
	}
	return n
}

func (ch *CrudHandler) seatRegistry() *streamSeatRegistry {
	if ch.seats == nil {
		ch.seats = newStreamSeatRegistry()
	}
	return ch.seats
}

// streamSeatPrincipal is the identity the seat cap counts by: owner, else
// user, else tenant, else the one anonymous bucket "".
func (ch *CrudHandler) streamSeatPrincipal(r *http.Request, ownerID any) string {
	if ownerID != nil {
		return "owner:" + fmt.Sprint(ownerID)
	}
	if u, ok := handler.GetUser(r.Context()); ok && u != nil {
		if id, ok := u.(interface{ GetID() string }); ok {
			return "user:" + id.GetID()
		}
		return "user:" + fmt.Sprint(u)
	}
	if t := tenant.GetTenantID(r.Context()); t != "" {
		return "tenant:" + t
	}
	return ""
}

// WithEventStreamSeats sets the per-principal cap on concurrent EventStream
// connections: zero keeps the default of 16, a negative value lifts the cap.
func (ch *CrudHandler) WithEventStreamSeats(n int) *CrudHandler {
	ch.EventStreamSeats = n
	return ch
}

// WithEventStreamSeatOverflow selects what a principal at its seat cap gets
// for its next stream: stream.SeatOverflowRefuse (the default) answers 429
// with Retry-After, stream.SeatOverflowEvictOldest closes its oldest stream.
func (ch *CrudHandler) WithEventStreamSeatOverflow(p stream.SeatOverflowPolicy) *CrudHandler {
	ch.EventStreamSeatOverflow = p
	return ch
}
