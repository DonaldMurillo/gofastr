package island

import "testing"

// TestManager_DroppedUpdatesCounter pins I3: updates discarded because a
// client's SSE buffer is full increment DroppedUpdates, making the
// previously-silent loss observable.
func TestManager_DroppedUpdatesCounter(t *testing.T) {
	m := NewManager()
	_ = m.Subscribe("s1") // buffered (64), nothing draining it

	for i := 0; i < 64; i++ {
		m.PushUpdate(IslandUpdate{IslandID: "i", HTML: "x"}, "s1")
	}
	if got := m.DroppedUpdates(); got != 0 {
		t.Fatalf("no drops expected within buffer, got %d", got)
	}

	// Buffer full — this one is dropped and counted.
	m.PushUpdate(IslandUpdate{IslandID: "i", HTML: "x"}, "s1")
	if got := m.DroppedUpdates(); got != 1 {
		t.Errorf("DroppedUpdates = %d, want 1", got)
	}
}
