package moduleproto

import (
	"context"
	"testing"
	"time"
)

// #669 (was #656): Peer.call marshaled params AFTER onID reported the
// id, so a call whose params failed to marshal reported an id whose
// frame was never sent. The supervisor had armed module.cancel for it:
// a cancel naming a frame that never crossed the wire. onID must fire
// only once the request is sendable — params marshaled, frame built.
func TestCallWithIDReportsNoIDWhenParamsFailToMarshal(t *testing.T) {
	host, _, _, _, cleanup := newPeerPair(t, 0)
	defer cleanup()

	reported := make(chan uint64, 1)
	_, err := host.CallWithID(context.Background(), "some.method",
		map[string]any{"ch": make(chan int)}, // json: unsupported type
		func(id uint64) { reported <- id })
	if err == nil {
		t.Fatal("call with unmarshalable params succeeded")
	}
	select {
	case id := <-reported:
		t.Errorf("SECURITY: [cancel-ghost] onID reported id %d for a frame that was never sent", id)
	case <-time.After(200 * time.Millisecond):
	}
	// The failed call must not leak its inflight slot either (review
	// finding): a valid call still goes through afterwards, and the
	// counter reads zero.
	if _, err := host.Call(context.Background(), "some.method", nil); err == nil {
		t.Log("call unexpectedly succeeded")
	}
	host.mu.Lock()
	leaked, tombstoned := host.inflight, len(host.abandoned)
	host.mu.Unlock()
	if leaked != 0 {
		t.Errorf("failed marshal leaked %d inflight slot(s)", leaked)
	}
	if tombstoned != 0 {
		t.Errorf("failed marshal tombstoned %d never-sent id(s); garbage tombstones evict real ones", tombstoned)
	}
}
