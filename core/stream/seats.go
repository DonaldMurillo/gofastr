package stream

// defaultSeatsPerPrincipal is the per-principal stream cap a zero-value
// SSEBrokerConfig gets: the same default core/mcp's SSE notification
// stream uses, so the two seat policies in the tree read as one number.
// Sixteen covers multi-tab / multi-device use of one account while
// bounding what a single low-privilege principal can hold resident
// (each seat is a goroutine plus a buffered channel).
const defaultSeatsPerPrincipal = 16

// SeatOverflowPolicy selects what a principal at MaxSeatsPerPrincipal
// does with its next stream.
type SeatOverflowPolicy uint8

const (
	// SeatOverflowRefuse answers the connection with 429 and holds no
	// seat — distinguishable from the global MaxSubscribers' 503, which
	// is about the server, not the caller.
	SeatOverflowRefuse SeatOverflowPolicy = iota
	// SeatOverflowEvictOldest closes that principal's oldest stream and
	// seats the new one. The reconnect-friendly policy: a client whose
	// previous connection is half-open (mobile handoff, laptop sleep)
	// gets its new stream instead of a 429 until the old seat's
	// heartbeat write fails.
	SeatOverflowEvictOldest
)

// seatsFor returns the resolved per-principal cap; negative means
// unlimited.
func seatsFor(n int) int {
	if n == 0 {
		return defaultSeatsPerPrincipal
	}
	return n
}

// SpliceSeat removes v from order[key]'s FIFO and deletes the key once the
// FIFO is empty. The caller holds whatever mutex guards order. A no-op when
// v is no longer in the FIFO, so a seat removed by an eviction path and
// again by its own departure is freed exactly once and the FIFO never
// retains a departed member.
//
// It is the canonical splice formerly duplicated as the spliceSeatLocked
// method on core/stream's SSEBroker, core/mcp's Server, and framework/
// crud's streamSeatRegistry (which all delegate to it).
func SpliceSeat[T comparable](order map[string][]T, key string, v T) {
	q := order[key]
	for i, item := range q {
		if item == v {
			order[key] = append(q[:i], q[i+1:]...)
			break
		}
	}
	if len(order[key]) == 0 {
		delete(order, key)
	}
}

// SeatMember is the shape AdmitSeat needs from a seat type: comparable
// identity (for the splice) and the channel closed to evict it; the
// principal a seat is counted under is AdmitSeat's own parameter. Satisfied by pointer seat types
// such as framework/crud's streamSeat and the agent-harness control
// plane's Seat.
type SeatMember interface {
	comparable
	SeatDone() chan struct{}
}

// AdmitSeat seats one member for principal under the per-principal cap
// n (0 = the shared default, negative = unlimited) and the overflow
// policy, appending it to order. newSeat builds the member. The bool is
// false when the principal is at the cap and the policy is Refuse;
// under EvictOldest the principal's oldest member is spliced from the
// FIFO and its done channel closed in the same step, then the new
// member is seated. The caller holds whatever mutex guards order.
//
// It replaces the acquire bodies formerly duplicated as
// framework/crud's streamSeatRegistry.admit and the harness control
// plane's SeatTable.AcquireSeat, which differed only in names.
func AdmitSeat[S SeatMember](order map[string][]S, principal string, n int, overflow SeatOverflowPolicy, newSeat func(principal string) S) (S, bool) {
	var zero S
	if seats := seatsFor(n); seats > 0 && len(order[principal]) >= seats {
		if overflow != SeatOverflowEvictOldest || len(order[principal]) == 0 {
			return zero, false
		}
		oldest := order[principal][0]
		SpliceSeat(order, principal, oldest)
		close(oldest.SeatDone())
	}
	seat := newSeat(principal)
	order[principal] = append(order[principal], seat)
	return seat, true
}

// spliceSeatLocked removes sub from its principal's seat FIFO; the
// caller holds b.mu. Used by Subscribe's unregister defer (voluntary
// departure), the replacement path, and the EvictOldest admission, so a
// seat is freed exactly once and the FIFO never retains a departed
// subscriber.
func (b *SSEBroker) spliceSeatLocked(sub *subscriber) {
	SpliceSeat(b.seatOrder, sub.principal, sub)
}
