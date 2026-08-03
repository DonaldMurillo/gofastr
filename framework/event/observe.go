package event

import "sync/atomic"

// Emission is one event that was published on a bus.
//
// This is separate from the bus's internal tap (setTap), which is a
// single-slot mechanism the outbox/fanout bridge already owns. Coverage
// recording must not compete for it.
type Emission struct {
	// Type is the event type string, e.g. "order.placed".
	Type string
	// Subscribers is how many handlers were registered for it at emit
	// time. Zero means the event went nowhere — published, and nothing
	// listening. That is worth distinguishing from "never published".
	Subscribers int
}

// observer is the process-wide emission hook. An atomic pointer because
// Emit runs on the request path whenever the bus is wired into an
// AfterCreate hook; an uninstalled observer costs one atomic load.
//
// A hook rather than a direct import of the coverage recorder, matching
// router.SetServeHook, access.SetObserver, and hook.SetObserver: event is
// a leaf package and the recorder is test tooling.
var observer atomic.Pointer[func(Emission)]

// SetObserver installs a callback fired for every event published through
// Emit or EmitStrict. Pass nil to clear. Intended for test tooling — the
// framework's semantic coverage recorder is the first consumer.
//
// The callback runs inline, so it must be cheap, must not block, and must
// tolerate concurrent calls.
func SetObserver(fn func(Emission)) {
	if fn == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&fn)
}

// observeEmission reports a publication to the installed observer.
func observeEmission(eventType string, subscribers int) {
	if fn := observer.Load(); fn != nil {
		(*fn)(Emission{Type: eventType, Subscribers: subscribers})
	}
}
