package rtc

import (
	"strings"
	"testing"
)

func TestRemotePeerEntryIsBounded(t *testing.T) {
	s := newTestSignaler(t, Config{})
	for _, p := range []PeerInfo{
		{ID: ""}, {ID: "a b"}, {ID: "p", User: "u\n"},
		{ID: "p", Status: []byte(`[1]`)}, {ID: "p", Status: []byte(`{"x":` + strings.Repeat("1", s.cfg.MaxStatusBytes) + `}`)},
	} {
		if s.validRemotePeer(p) {
			t.Errorf("validRemotePeer(%+v) = true, want the entry refused", p)
		}
	}
	if !s.validRemotePeer(PeerInfo{ID: "p", Status: []byte(`{"m":1}`)}) {
		t.Fatal("a well-formed remote entry was refused")
	}
}

func TestValidJoinRefusesEachBadField(t *testing.T) {
	bad := []Join{
		{Room: ""}, {Room: "has space"}, {Room: strings.Repeat("r", 129)},
		{Room: "room", PeerID: "bad id"}, {Room: "room", Role: strings.Repeat("x", 33)},
		{Room: "room", User: "u\x00"}, {Room: "room", User: strings.Repeat("u", 129)},
		{Room: "room", DisplayName: "bad\x01name"}, {Room: "room", DisplayName: strings.Repeat("n", 65)},
	}
	for _, j := range bad {
		if validJoin(j) == nil {
			t.Errorf("validJoin(%+v) = nil, want a refusal", j)
		}
	}
	if err := validJoin(Join{Room: "room-1", User: "u1", PeerID: "p1"}); err != nil {
		t.Fatalf("a well-formed Join was refused: %v", err)
	}
}
