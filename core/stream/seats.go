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

// spliceSeatLocked removes sub from its principal's seat FIFO; the
// caller holds b.mu. Used by Subscribe's unregister defer (voluntary
// departure), the replacement path, and the EvictOldest admission, so a
// seat is freed exactly once and the FIFO never retains a departed
// subscriber.
func (b *SSEBroker) spliceSeatLocked(sub *subscriber) {
	q := b.seatOrder[sub.principal]
	for i, v := range q {
		if v == sub {
			b.seatOrder[sub.principal] = append(q[:i], q[i+1:]...)
			break
		}
	}
	if len(b.seatOrder[sub.principal]) == 0 {
		delete(b.seatOrder, sub.principal)
	}
}
