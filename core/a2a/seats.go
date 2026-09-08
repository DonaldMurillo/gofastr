package a2a

import (
	"sync"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

// defaultStreamSeatsPerOwner is the per-principal cap a zero-value
// Config gets: the same default core/stream's SSE broker, core/mcp's
// SSE notification stream, and the crud event stream use, so every seat
// policy in the tree reads as one number. Sixteen covers multi-tab /
// multi-device use of one account while bounding what a single
// low-privilege principal can hold resident (each stream is a forward or
// poll loop goroutine, a bus channel, and two tickers).
const defaultStreamSeatsPerOwner = 16

// streamSeat is one admitted SSE stream (SubscribeToTask or
// SendStreamingMessage), held from admission until the handler returns.
// done is closed exactly once — by an EvictOldest admission evicting
// this seat, or by release at the stream's own departure — and the
// stream's relay loop treats it as "evicted: end the stream".
type streamSeat struct {
	owner     string
	done      chan struct{}
	closeOnce sync.Once
	tracked   bool // false for unlimited-cap servers: no FIFO entry
}

// streamSeatRegistry tracks live stream seats per owner as a FIFO, the
// shape the SeatOverflowEvictOldest policy pops from. One registry per
// Server; guarded by its own mutex so admission never contends with the
// run registry.
type streamSeatRegistry struct {
	mu    sync.Mutex
	order map[string][]*streamSeat
}

func newStreamSeatRegistry() *streamSeatRegistry {
	return &streamSeatRegistry{order: make(map[string][]*streamSeat)}
}

// admitSeat seats one stream for owner under the server's cap and overflow
// policy. The bool is false when the owner is at the cap and the policy
// is Refuse; the caller answers 429 and holds nothing. Under
// EvictOldest the owner's oldest seat is closed and removed in the same
// critical section, and the new stream is seated.
func (r *streamSeatRegistry) admitSeat(owner string, cap int, overflow stream.SeatOverflowPolicy) (*streamSeat, bool) {
	if cap < 0 {
		// Unlimited: a seat with no FIFO entry, so release is a no-op.
		return &streamSeat{done: make(chan struct{})}, true
	}
	if cap == 0 {
		cap = defaultStreamSeatsPerOwner
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.order[owner]
	if len(q) >= cap {
		if overflow != stream.SeatOverflowEvictOldest || len(q) == 0 {
			return nil, false
		}
		old := q[0]
		q = q[1:]
		old.closeOnce.Do(func() { close(old.done) })
	}
	seat := &streamSeat{owner: owner, done: make(chan struct{}), tracked: true}
	r.order[owner] = append(q, seat)
	return seat, true
}

// releaseSeat departs a seat: the stream's handler returned (client gone,
// task settled, or an EvictOldest admission already ended it via done).
// Splicing is idempotent, so releasing a seat an eviction already
// removed from the FIFO is a no-op beyond closing done.
func (r *streamSeatRegistry) releaseSeat(seat *streamSeat) {
	seat.closeOnce.Do(func() { close(seat.done) })
	if !seat.tracked {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.order[seat.owner]
	for i, v := range q {
		if v == seat {
			r.order[seat.owner] = append(q[:i], q[i+1:]...)
			break
		}
	}
	if len(r.order[seat.owner]) == 0 {
		delete(r.order, seat.owner)
	}
}

// liveStreamSeats returns the number of seats currently held, across
// owners. The disconnect-accounting observable for tests: after every
// stream ends it must return to zero on a fresh server.
func (r *streamSeatRegistry) liveStreamSeats() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, q := range r.order {
		n += len(q)
	}
	return n
}
