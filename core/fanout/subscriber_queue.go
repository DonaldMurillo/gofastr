package fanout

import (
	"log/slog"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func deliver(fn func([]byte), p []byte) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("fanout: subscriber panicked — payload dropped",
				slog.Any("panic", textsafe.Recovered(r)))
		}
	}()
	fn(p)
}

// SubscriberQueue runs fn on its own goroutine behind a bounded drop-oldest
// queue whose send never blocks; depth <= 0 selects the default.
func SubscriberQueue(fn func([]byte), depth int) (send func([]byte), stop func()) {
	sendItem, stop := dropOldestQueue(func(p []byte) { deliver(fn, p) }, depth, nil)
	send = func(payload []byte) {
		sendItem(append([]byte(nil), payload...))
	}
	return send, stop
}

func dropOldestQueue[T any](fn func(T), depth int, dropped func(T)) (send func(T), stop func()) {
	if depth <= 0 {
		depth = defaultInProcessQueue
	}
	q := make(chan T, depth)
	stopped := make(chan struct{})
	var once sync.Once
	isStopped := func() bool {
		select {
		case <-stopped:
			return true
		default:
			return false
		}
	}
	go func() {
		for {
			select {
			case <-stopped:
				return
			case item := <-q:
				if isStopped() {
					return
				}
				fn(item)
			}
		}
	}()
	send = func(item T) {
		for {
			select {
			case <-stopped:
				return
			case q <- item:
				return
			default:
			}
			select {
			case old := <-q:
				if dropped != nil {
					dropped(old)
				}
			case <-stopped:
				return
			default:
			}
		}
	}
	stop = func() {
		once.Do(func() { close(stopped) })
	}
	return send, stop
}
