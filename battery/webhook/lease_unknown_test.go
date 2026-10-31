package webhook

import (
	"testing"
	"time"
)

// A host store that claims rows itself leaves claimedUntil unset. Read as
// an expired lease, every claim went back unsent and nothing delivered.
func TestUnknownLeaseIsNotTooShort(t *testing.T) {
	m := New(NewMemoryStore(), Options{LeasePeriod: time.Minute})
	if m.leaseTooShort(Delivery{}) {
		t.Fatal("a claim with no lease was refused as too short")
	}
	if !m.leaseTooShort(Delivery{claimedUntil: time.Now().Add(time.Second)}) {
		t.Fatal("a lease one second from expiry was started")
	}
}
