package event

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
)

const fanoutTopic = "gofastr.events"

const bridgeQueueDepth = 256

const bridgePublishTimeout = 5 * time.Second

// AttachFanout publishes bus's events to f and re-emits other replicas' events
// on bus, lossy and never blocking an emitter. It errors on a nil fanout or a
// second attach; stop detaches it and is idempotent.
func AttachFanout(bus *EventBus, f fanout.Fanout) (stop func(), err error) {
	if f == nil {
		return nil, fmt.Errorf("event: AttachFanout: nil fanout")
	}
	bus.mu.Lock()
	if bus.fanoutAttached {
		bus.mu.Unlock()
		return nil, fmt.Errorf("event: AttachFanout: fanout already attached")
	}
	bus.fanoutAttached = true
	bus.mu.Unlock()

	nodeID := fanout.NewNodeID()
	cancel, err := f.Subscribe(fanoutTopic, func(raw []byte) {
		_, body, ok := fanout.UnwrapForeign(nodeID, raw)
		if !ok {
			return
		}
		var ev Event
		if err := jsonexact.UnmarshalFullRange(body, &ev); err != nil {
			return
		}
		// Re-emit locally with the tap suppressed so this is not rebroadcast.
		bus.EmitAsync(withRemoteReemit(context.Background()), ev)
	})
	if err != nil {
		bus.mu.Lock()
		bus.fanoutAttached = false
		bus.mu.Unlock()
		return nil, fmt.Errorf("event: AttachFanout: subscribe %q: %w", fanoutTopic, err)
	}

	var (
		mu               sync.Mutex
		marshalErrLogged bool
	)
	logMarshalErrOnce := func(eventType string, err error) {
		mu.Lock()
		defer mu.Unlock()
		if marshalErrLogged {
			return
		}
		marshalErrLogged = true
		slog.Default().Error(
			"event: fanout bridge skipped non-marshalable event data; later events still publish (logging once; later marshal failures are silent)",
			"topic", fanoutTopic, "type", eventType, "err", err,
		)
	}

	queue := make(chan []byte, bridgeQueueDepth)
	publisherCtx, stopPublisher := context.WithCancel(context.Background())
	publisherDone := make(chan struct{})
	var overflowOnce sync.Once
	logOverflowOnce := func() {
		overflowOnce.Do(func() {
			slog.Default().Warn("event: fanout bridge publish queue overflowed; dropping envelopes (lossy real-time lane)", "topic", fanoutTopic)
		})
	}
	go func() {
		defer close(publisherDone)
		for publisherCtx.Err() == nil {
			select {
			case <-publisherCtx.Done():
				return
			case data := <-queue:
				ctx, cancel := context.WithTimeout(publisherCtx, bridgePublishTimeout)
				publishSafely(ctx, f, data)
				cancel()
			}
		}
	}()

	tap := func(ctx context.Context, ev Event) {
		if IsRemote(ctx) {
			return
		}
		data, err := marshalEvent(ev)
		if err != nil {
			logMarshalErrOnce(ev.Type, err)
			return
		}
		wrapped := fanout.Wrap(nodeID, data)
		select {
		case queue <- wrapped:
		default:
			select {
			case <-queue:
				logOverflowOnce()
			default:
			}
			select {
			case queue <- wrapped:
			default:
				logOverflowOnce()
			}
		}
	}
	clearTap := bus.setTap(tap)

	var once sync.Once
	return func() {
		once.Do(func() {
			clearTap()
			stopPublisher()
			cancel()
			<-publisherDone
			bus.mu.Lock()
			bus.fanoutAttached = false
			bus.mu.Unlock()
		})
	}, nil
}

func marshalEvent(ev Event) (data []byte, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			data, err = nil, fmt.Errorf("event data marshaler panicked: %s", textsafe.Recovered(rec))
		}
	}()
	return json.Marshal(ev)
}

func publishSafely(ctx context.Context, f fanout.Fanout, data []byte) {
	defer func() {
		if r := recover(); r != nil {
			slog.Default().Error("event: fanout publish panicked (best-effort lane; continuing)",
				"topic", fanoutTopic, "panic", textsafe.Recovered(r), "stack", string(debug.Stack()))
		}
	}()
	if perr := f.Publish(ctx, fanoutTopic, data); perr != nil {
		slog.Default().Debug("event: fanout publish failed (best-effort)", "topic", fanoutTopic, "err", perr)
	}
}
