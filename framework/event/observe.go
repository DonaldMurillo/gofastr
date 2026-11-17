package event

import (
	"context"
	"sync/atomic"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Emission is one event published on a bus, as SetObserver reports it.
type Emission struct {
	// Type is the event type.
	Type string
	// Subscribers is how many handlers the type had at emit time.
	Subscribers int
}

var observer atomic.Pointer[func(Emission)]

// SetObserver installs fn to be called inline, from any goroutine, for every
// Emit and EmitStrict; nil clears it. A panic in fn is logged and ignored.
func SetObserver(fn func(Emission)) {
	if fn == nil {
		observer.Store(nil)
		return
	}
	observer.Store(&fn)
}

func observeEmission(eventType string, subscribers int) {
	if fn := observer.Load(); fn != nil {
		func() {
			defer textsafe.LogPanic(context.Background(), "event: observer panicked; ignored")
			(*fn)(Emission{Type: eventType, Subscribers: subscribers})
		}()
	}
}
