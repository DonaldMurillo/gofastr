// Package genericdisp pins the generic-dispatch leg (2026-09-07 round,
// the StateChannel probe): a module-declared GENERIC interface whose
// implementations the host installs, dispatched from a generic struct's
// select loop. go/types resolves selectors on generic instantiations to
// per-instantiation method objects, so without origin canonicalization
// the dispatch loop's edges vanish and the callbacks under it never go
// hot — the exact blind spot the probe drove.
package genericdisp

// Source mirrors stream.SnapshotSource: a module-declared generic
// extension point the host implements.
type Source[Role comparable, Snapshot any, Event any] interface {
	SnapshotFor(role Role) (Snapshot, uint64, bool)
	FilterEvent(role Role, event Event) (payload any, ok bool)
}

type job[Role comparable, Event any] struct {
	kind  int
	role  Role
	event Event
}

// Channel mirrors stream.StateChannel: the source is a struct field the
// constructor installs from a host parameter.
type Channel[Role comparable, Snapshot any, Event any] struct {
	source Source[Role, Snapshot, Event]
	stop   chan struct{}
	jobs   chan job[Role, Event]
}

func NewChannel[Role comparable, Snapshot any, Event any](src Source[Role, Snapshot, Event]) *Channel[Role, Snapshot, Event] {
	return &Channel[Role, Snapshot, Event]{source: src}
}

// Run is the dispatch loop: a channel-receive select, the read-loop
// seed. Its calls into the generic receiver's own methods must resolve
// to the DECLARED methods for the flood to reach runSnapshot/deliver.
func (c *Channel[Role, Snapshot, Event]) Run() {
	for {
		select {
		case <-c.stop:
			return
		case j := <-c.jobs:
			if j.kind == 0 {
				c.runSnapshot(j)
			} else {
				c.runEvent(j)
			}
		}
	}
}

func (c *Channel[Role, Snapshot, Event]) runSnapshot(j job[Role, Event]) {
	_, _, _ = c.source.SnapshotFor(j.role) // want `recovercallback: c\.source\.SnapshotFor is invoked with no recover in scope`
}

func (c *Channel[Role, Snapshot, Event]) runEvent(j job[Role, Event]) {
	c.deliver(j)
}

func (c *Channel[Role, Snapshot, Event]) deliver(j job[Role, Event]) {
	_, _ = c.source.FilterEvent(j.role, j.event) // want `recovercallback: c\.source\.FilterEvent is invoked with no recover in scope`
}

// Guarded is the fix posture: the same generic dispatch with a deferred
// recover on the panicking frame, quiet.
type Guarded[Role comparable, Snapshot any, Event any] struct {
	source Source[Role, Snapshot, Event]
	jobs   chan job[Role, Event]
}

func (g *Guarded[Role, Snapshot, Event]) Run() {
	for j := range g.jobs {
		g.dispatch(j)
	}
}

func (g *Guarded[Role, Snapshot, Event]) dispatch(j job[Role, Event]) {
	defer func() { _ = recover() }()
	if j.kind == 0 {
		_, _, _ = g.source.SnapshotFor(j.role)
	} else {
		_, _ = g.source.FilterEvent(j.role, j.event)
	}
}
