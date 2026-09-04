// Package bridgeish mirrors framework/event's AttachFanout publisher
// goroutine, reduced to the shape: a backend parameter of ANOTHER
// module package's interface (fanoutish.Fanout standing in for
// core/fanout.Fanout), drained from a goroutine literal with no
// recover. badAttach is the pre-fix bridge.go:105-119; attachFixed is
// the fix posture. depish pins the third-party silence and registry
// pins the map-entry receiver.
package bridgeish

import (
	"context"
	"errors"
	"time"

	"example.app/fanoutish"
	"example.org/depish"
)

const topic = "gofastr.events"

// badAttach is the pre-fix shape: f.Publish inside the publisher
// goroutine (bridge.go:113), plus the local-copy spelling beside it.
func badAttach(f fanoutish.Fanout, d depish.Dep) (stop func()) {
	queue := make(chan []byte, 8)
	stopped := make(chan struct{})
	pub := f
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stopped:
				return
			case data := <-queue:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = f.Publish(ctx, topic, data)   // want `recovercallback: f\.Publish is invoked with no recover in scope`
				_ = pub.Publish(ctx, topic, data) // want `recovercallback: pub\.Publish is invoked with no recover in scope`
				_ = d.Do(ctx)                     // quiet: a third-party interface, not this repo's extension point
				cancel()
			}
		}
	}()
	return func() { close(stopped) }
}

// attachFixed is the fix posture: Publish runs inside a recovered
// helper on the same goroutine, so a panicking backend is contained.
func attachFixed(f fanoutish.Fanout) (stop func()) {
	queue := make(chan []byte, 8)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stopped:
				return
			case data := <-queue:
				_ = publishSafe(f, data)
			}
		}
	}()
	return func() { close(stopped) }
}

func publishSafe(f fanoutish.Fanout, data []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("fanout publish panicked")
		}
	}()
	return f.Publish(context.Background(), topic, data)
}

// registry holds backends in a map, the registry spelling of the
// extension-point receiver.
type registry struct {
	backends map[string]fanoutish.Fanout
}

func (r *registry) drain(queue chan []byte) {
	for data := range queue {
		be := r.backends["a"]
		_ = be.Publish(context.Background(), topic, data) // want `recovercallback: be\.Publish is invoked with no recover in scope`
		// The guarded twin on the same hot loop: publishSafe's own
		// recover is what keeps it quiet.
		_ = publishSafe(r.backends["b"], data)
	}
}

// syncPublish stays quiet: a synchronous caller with a recover net of
// its own choosing — reachability, not adjacency, is the test.
func syncPublish(f fanoutish.Fanout, data []byte) error {
	return f.Publish(context.Background(), topic, data)
}
