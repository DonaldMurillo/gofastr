// Package a holds the discardederr fixture reduced from the real bug
// site: core-ui/island manager.go Subscribe as it was before fix
// e936f791 (probe TestSubscribeCapRefusalObservable).
package a

// IslandUpdate is one swapped island payload.
type IslandUpdate struct {
	HTML string
}

// Manager holds the session streams, reduced.
type Manager struct{}

// subscribeImpl is the cap-enforcing chokepoint: a refused connect
// returns a nil channel, a no-op cancel, and a sentinel error.
func (m *Manager) subscribeImpl(sessionID string) (<-chan IslandUpdate, func(), error) {
	return nil, func() {}, nil
}

// Subscribe, pre-fix: the refusal was dropped, and the documented
// `for upd := range ch` consume pattern hung forever on a channel that
// would never deliver and never close.
func (m *Manager) Subscribe(sessionID string) (<-chan IslandUpdate, func()) {
	ch, cancel, _ := m.subscribeImpl(sessionID) // want `assignment discards the error from subscribeImpl`
	return ch, cancel
}

// SubscribeFixed is the fix posture: surface the refusal as a closed
// channel so the documented consume pattern terminates.
func (m *Manager) SubscribeFixed(sessionID string) (<-chan IslandUpdate, func()) {
	ch, cancel, err := m.subscribeImpl(sessionID)
	if err != nil {
		refused := make(chan IslandUpdate)
		close(refused)
		return refused, cancel
	}
	return ch, cancel
}

// fieldKeep: the kept results land in the receiver's fields — a nil
// channel and a no-op cancel still march on.
type holder struct {
	ch     <-chan IslandUpdate
	cancel func()
}

func (h *holder) fieldKeep(m *Manager, id string) {
	h.ch, h.cancel, _ = m.subscribeImpl(id) // want `assignment discards the error from subscribeImpl`
	for upd := range h.ch {
		_ = upd
	}
}

// overwrittenBoth: every kept value is only ever overwritten or
// explicitly discarded — nothing marches on with the refusal hidden.
func (m *Manager) overwrittenBoth(id string) {
	ch, cancel, _ := m.subscribeImpl(id)
	ch, cancel, _ = m.subscribeImpl(id)
	_ = ch
	_ = cancel
}

// keepThenReassign: a second fetch overwrites both, and the NEW values
// are used — the first drop is still the swallowed refusal.
func (m *Manager) keepThenReassign(id string) (<-chan IslandUpdate, func()) {
	ch, cancel, _ := m.subscribeImpl(id) // want `assignment discards the error from subscribeImpl`
	ch, cancel, _ = m.subscribeImpl(id)  // want `assignment discards the error from subscribeImpl`
	return ch, cancel
}
