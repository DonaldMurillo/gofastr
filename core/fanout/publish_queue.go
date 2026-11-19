package fanout

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const publishTimeout = 5 * time.Second

// ErrPublishDropped reports a payload the queue dropped for a newer one.
var ErrPublishDropped = errors.New("fanout: publish queue full; message dropped")

// PublishQueue returns a send that never blocks and publishes to f on topic
// from one goroutine. stop must not be called holding a lock Publish takes.
func PublishQueue(f Fanout, topic string, depth int) (send func([]byte), stop func()) {
	sendReporting, stop := PublishQueueReporting(f, topic, depth)
	return func(payload []byte) { sendReporting(payload, nil) }, stop
}

// PublishQueueReporting is [PublishQueue] whose failed callback runs once
// for each payload the running queue failed to publish, with the reason. A
// payload still queued, or sent, when stop runs is dropped without a call.
func PublishQueueReporting(f Fanout, topic string, depth int) (send func(payload []byte, failed func(error)), stop func()) {
	type item struct {
		payload []byte
		failed  func(error)
	}
	var (
		mu       sync.Mutex
		closed   bool
		inflight sync.WaitGroup
	)
	sendItem, stopQueue := dropOldestQueue(func(it item) {
		mu.Lock()
		if closed {
			mu.Unlock()
			return
		}
		inflight.Add(1)
		mu.Unlock()
		defer inflight.Done()
		if err := publishSafely(f, topic, it.payload); err != nil {
			slog.Default().Debug("fanout: publish failed; message dropped (lossy lane)",
				"topic", topic, "err", err)
			reportFailed(it.failed, err)
		}
	}, depth, func(it item) {
		reportFailed(it.failed, ErrPublishDropped)
	})
	send = func(payload []byte, failed func(error)) {
		sendItem(item{payload: append([]byte(nil), payload...), failed: failed})
	}
	stop = func() {
		stopQueue()
		mu.Lock()
		closed = true
		mu.Unlock()
		inflight.Wait()
	}
	return send, stop
}

var errPublishPanicked = errors.New("fanout: publish panicked; message dropped")

func publishSafely(f Fanout, topic string, payload []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("fanout: publish panicked; message dropped",
				"topic", topic, slog.Any("panic", textsafe.Recovered(r)))
			err = errPublishPanicked
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), publishTimeout)
	defer cancel()
	return f.Publish(ctx, topic, payload)
}

func reportFailed(failed func(error), err error) {
	if failed == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("fanout: publish failure callback panicked",
				slog.Any("panic", textsafe.Recovered(r)))
		}
	}()
	failed(err)
}
