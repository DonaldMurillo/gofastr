package control

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

func TestUnmarshalCommandRefusesAmbiguousKeysAtAnyDepth(t *testing.T) {
	bad := map[string]string{
		"duplicate envelope key": `{"kind":"CancelTurn","kind":"SetModel","body":{}}`,
		"case-folded envelope":   `{"kind":"CancelTurn","KIND":"SetModel","body":{}}`,
		"duplicate body key":     `{"kind":"SetModel","body":{"model":"a","model":"b"}}`,
		"case-folded body key":   `{"kind":"SetModel","body":{"model":"a","MODEL":"b"}}`,
		"nested duplicate":       `{"kind":"SendInput","body":{"content":[{"type":"text","text":"a","text":"b"}]}}`,
	}
	for name, in := range bad {
		if _, err := UnmarshalCommand([]byte(in)); err == nil {
			t.Errorf("%s: UnmarshalCommand accepted %s", name, in)
		}
	}
	got, err := UnmarshalCommand([]byte(`{"kind":"SetModel","body":{"model":"m","futureField":1}}`))
	if err != nil {
		t.Fatalf("unknown body field refused: %v", err)
	}
	if got.(SetModel).Model != "m" {
		t.Fatalf("got %+v", got)
	}
}

func TestKindListsMatchCodecs(t *testing.T) {
	for _, k := range AllCommandKinds() {
		c, err := UnmarshalCommand([]byte(`{"kind":"` + k + `","body":{}}`))
		if err != nil {
			t.Errorf("command kind %q in AllCommandKinds does not decode: %v", k, err)
			continue
		}
		if c.CommandKind() != k {
			t.Errorf("command kind %q decodes to %q", k, c.CommandKind())
		}
	}
	for _, k := range AllEventKinds() {
		e, err := DecodeEvent(EventEnvelope{Kind: k, Payload: json.RawMessage(`{}`)})
		if err != nil {
			t.Errorf("event kind %q in AllEventKinds does not decode: %v", k, err)
			continue
		}
		if e.EventKind() != k {
			t.Errorf("event kind %q decodes to %q", k, e.EventKind())
		}
	}
	if _, err := DecodeEvent(EventEnvelope{Kind: "NotAThing", Payload: json.RawMessage(`{}`)}); err == nil {
		t.Error("DecodeEvent accepted an unknown kind")
	}
}

func TestIdentityClassRefusesUnknownWireString(t *testing.T) {
	var c IdentityClass
	if err := json.Unmarshal([]byte(`"robot"`), &c); err == nil {
		t.Fatal("unknown identity_class accepted")
	}
	if IdentityClass(9).String() != "unknown" {
		t.Fatal("out-of-range class must stringify as unknown")
	}
}

func TestSeatTableCapAndEviction(t *testing.T) {
	tb := NewSeatTable()
	for i := 0; i < DefaultStreamSeats; i++ {
		if _, ok := tb.AcquireSeat("p", 0, stream.SeatOverflowRefuse); !ok {
			t.Fatalf("seat %d refused under the default cap", i)
		}
	}
	if _, ok := tb.AcquireSeat("p", 0, stream.SeatOverflowRefuse); ok {
		t.Fatal("seat past DefaultStreamSeats admitted under Refuse")
	}
	if _, ok := tb.AcquireSeat("q", 0, stream.SeatOverflowRefuse); !ok {
		t.Fatal("cap is per principal: another principal was refused")
	}

	ref := map[string][]*Seat{}
	n := 0
	for {
		if _, ok := stream.AdmitSeat(ref, "p", 0, stream.SeatOverflowRefuse, func(p string) *Seat { return &Seat{principal: p, Done: make(chan struct{})} }); !ok {
			break
		}
		n++
	}
	if n != DefaultStreamSeats {
		t.Fatalf("DefaultStreamSeats = %d, core/stream default cap = %d", DefaultStreamSeats, n)
	}

	un := NewSeatTable()
	for i := 0; i < DefaultStreamSeats*2; i++ {
		if _, ok := un.AcquireSeat("p", -1, stream.SeatOverflowRefuse); !ok {
			t.Fatal("negative cap must be unlimited")
		}
	}

	ev := NewSeatTable()
	first, _ := ev.AcquireSeat("p", 1, stream.SeatOverflowEvictOldest)
	second, ok := ev.AcquireSeat("p", 1, stream.SeatOverflowEvictOldest)
	if !ok {
		t.Fatal("EvictOldest refused")
	}
	select {
	case <-first.Done:
	default:
		t.Fatal("evicted seat's Done not closed")
	}
	ev.ReleaseSeat(first)
	ev.ReleaseSeat(second)
	ev.ReleaseSeat(second)
	if len(ev.order["p"]) != 0 {
		t.Fatalf("FIFO retains departed seats: %d", len(ev.order["p"]))
	}
	if _, ok := ev.AcquireSeat("p", 1, stream.SeatOverflowRefuse); !ok {
		t.Fatal("released seat still counted")
	}
	if DefaultSeatTable() != DefaultSeatTable() {
		t.Fatal("DefaultSeatTable is not process-wide")
	}
}
