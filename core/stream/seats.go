package stream

const defaultSeatsPerPrincipal = 16

// SeatOverflowPolicy selects what a principal at MaxSeatsPerPrincipal
// does with its next stream.
type SeatOverflowPolicy uint8

const (
	// SeatOverflowRefuse answers the new stream 429.
	SeatOverflowRefuse SeatOverflowPolicy = iota
	// SeatOverflowEvictOldest closes the principal's oldest stream and seats the new one.
	SeatOverflowEvictOldest
)

func seatsFor(n int) int {
	if n == 0 {
		return defaultSeatsPerPrincipal
	}
	return n
}

// SpliceSeat removes v from order[key] and deletes the key once it is
// empty; a no-op when v is not there. The caller holds the lock on order.
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

// SeatMember is a seat type AdmitSeat can hold: comparable, with the
// channel closed to evict it.
type SeatMember interface {
	comparable
	SeatDone() chan struct{}
}

// AdmitSeat seats a member for principal under cap n (0 = 16, negative =
// no cap). At the cap it returns false under SeatOverflowRefuse; under
// SeatOverflowEvictOldest it splices the oldest member and closes its
// SeatDone channel first. The caller holds the lock on order.
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

func (b *SSEBroker) spliceSeatLocked(sub *subscriber) {
	SpliceSeat(b.seatOrder, sub.principal, sub)
}
