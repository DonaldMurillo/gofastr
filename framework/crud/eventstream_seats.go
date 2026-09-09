package crud

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/stream"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// defaultEventStreamSeats is the per-principal stream cap a zero-value
// CrudHandler gets: the same default core/stream's SSE broker and core/mcp's
// SSE notification stream use, so the three seat policies in the tree read as
// one number. Sixteen covers multi-tab / multi-device use of one account
// while bounding what a single low-privilege principal can hold resident
// (each stream is a goroutine, a 32-entry buffer, and three bus
// subscriptions until it disconnects).
const defaultEventStreamSeats = 16

// eventStreamSeatsFor resolves EventStreamSeats: 0 = the default,
// negative = unlimited (deployments that bound seats elsewhere).
func eventStreamSeatsFor(n int) int {
	if n == 0 {
		return defaultEventStreamSeats
	}
	return n
}

// streamSeat is one admitted EventStream connection, held from connect until
// the handler returns. done is closed exactly once, by whichever registry
// path removes the seat from the principal's FIFO (an EvictOldest admission
// or the stream's own departure through release), and the handler's select
// loop treats it as a disconnect.
type streamSeat struct {
	principal string
	done      chan struct{}
}

// SeatDone implements stream.SeatMember so the shared
// admission helper (stream.AdmitSeat) can count, evict, and close the
// seat.
func (s *streamSeat) SeatDone() chan struct{} { return s.done }

// streamSeatRegistry tracks live EventStream seats per principal as a FIFO,
// the shape the SeatOverflowEvictOldest policy pops from. It is a pointer
// field on CrudHandler so the tx-bound copies inTx makes share one registry:
// seats belong to the handler's stream surface, not to a transaction.
type streamSeatRegistry struct {
	mu    sync.Mutex
	order map[string][]*streamSeat
}

func newStreamSeatRegistry() *streamSeatRegistry {
	return &streamSeatRegistry{order: make(map[string][]*streamSeat)}
}

// spliceSeatLocked removes seat from its principal's FIFO; the caller holds
// r.mu. A no-op when the seat is already gone (released after an eviction,
// or evicted after a release), so a seat is removed exactly once and the
// FIFO never retains a departed stream. The splice itself is
// stream.SpliceSeat, shared with core/stream's broker and core/mcp's SSE
// notification stream.
func (r *streamSeatRegistry) spliceSeatLocked(seat *streamSeat) {
	stream.SpliceSeat(r.order, seat.principal, seat)
}

// admit seats one stream for principal under the handler's cap and overflow
// policy, returning the seat. The bool is false when the principal is at the
// cap and the policy is Refuse; the caller answers 429 with Retry-After
// (EventSource backs off) and holds nothing. Under EvictOldest the
// principal's oldest seat is closed and removed in the same critical
// section, and the new stream is seated. The body is the shared
// stream.AdmitSeat, which this registry formerly duplicated.
func (r *streamSeatRegistry) admit(principal string, cap int, overflow stream.SeatOverflowPolicy) (*streamSeat, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return stream.AdmitSeat(r.order, principal, eventStreamSeatsFor(cap), overflow,
		func(p string) *streamSeat { return &streamSeat{principal: p, done: make(chan struct{})} })
}

// release departs a seat: voluntary disconnect, write error, or a re-auth
// refusal closing the stream. Splicing is idempotent, so releasing a seat an
// EvictOldest admission already removed is a no-op.
func (r *streamSeatRegistry) release(seat *streamSeat) {
	r.mu.Lock()
	r.spliceSeatLocked(seat)
	r.mu.Unlock()
}

// liveSeats returns the number of seats currently held, across principals.
// The disconnect-accounting observable for tests: after every stream ends it
// must return to zero on a fresh handler.
func (r *streamSeatRegistry) liveSeats() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, q := range r.order {
		n += len(q)
	}
	return n
}

// seatRegistry returns the handler's stream-seat registry, allocating it the
// first time EventStream is mounted (NewCrudHandler pre-allocates for the
// normal path; this covers hand-built CrudHandler literals). Route mounting
// happens before serving, so the write races nothing.
func (ch *CrudHandler) seatRegistry() *streamSeatRegistry {
	if ch.seats == nil {
		ch.seats = newStreamSeatRegistry()
	}
	return ch.seats
}

// streamSeatPrincipal derives the caller identity the seat cap counts by:
// the owner id for OwnerField entities, else the authenticated user, else
// the tenant, else "". Anonymous callers on a Public stream share ONE bucket
// (""), so the cap bounds them collectively — the same posture core/stream's
// broker takes when it has no way to tell callers apart.
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
// connections. Zero keeps the default (16); a negative value lifts the cap
// for deployments that bound seats elsewhere. Values are taken verbatim —
// the resolution rules live in eventStreamSeatsFor.
func (ch *CrudHandler) WithEventStreamSeats(n int) *CrudHandler {
	ch.EventStreamSeats = n
	return ch
}

// WithEventStreamSeatOverflow selects what a principal at its seat cap does
// with its next stream: stream.SeatOverflowRefuse (the default) answers 429
// with Retry-After at connect, stream.SeatOverflowEvictOldest closes that
// principal's oldest stream and seats the new one — the multi-tab-friendly
// policy for apps whose clients reconnect faster than a half-open
// connection's seat is reclaimed by its re-auth ticker.
func (ch *CrudHandler) WithEventStreamSeatOverflow(p stream.SeatOverflowPolicy) *CrudHandler {
	ch.EventStreamSeatOverflow = p
	return ch
}
