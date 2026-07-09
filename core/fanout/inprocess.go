package fanout

import (
	"context"
	"sync"
)

// defaultInProcessQueue is the per-subscriber queue depth. A subscriber that
// can't keep up drops oldest, matching [stream.SSEBroker]'s lossy contract.
const defaultInProcessQueue = 256

// InProcess is an in-memory [Fanout]. Its primary purpose is tests: wiring
// two buses / two island managers / two brokers to one InProcess simulates
// two replicas inside a single test binary. It is fully concurrency-safe and
// delivers per-subscriber in publish order.
type InProcess struct {
	mu         sync.RWMutex
	nextSubID  uint64
	topics     map[string]map[uint64]*ipSubscriber
	queueDepth int
}

// ipSubscriber pairs a registration identity with its per-subscriber queue;
// the dedicated goroutine + drop-oldest machinery lives in [SubscriberQueue].
type ipSubscriber struct {
	id    uint64
	topic string
	send  func([]byte)
	stop  func()
}

// InProcessOption configures an [InProcess].
type InProcessOption func(*InProcess)

// WithInProcessQueue overrides the per-subscriber bounded queue depth
// (default 256). A depth of 0 keeps the default. Mainly useful in tests that
// need to exercise the drop-oldest overflow path quickly.
func WithInProcessQueue(depth int) InProcessOption {
	return func(ip *InProcess) {
		if depth > 0 {
			ip.queueDepth = depth
		}
	}
}

// NewInProcess returns a concurrency-safe in-memory fanout.
func NewInProcess(opts ...InProcessOption) *InProcess {
	ip := &InProcess{
		topics:     map[string]map[uint64]*ipSubscriber{},
		queueDepth: defaultInProcessQueue,
	}
	for _, opt := range opts {
		opt(ip)
	}
	return ip
}

// Publish broadcasts payload to every subscriber of topic. It never blocks:
// a subscriber whose queue is full has its oldest queued message dropped to
// make room (mirroring [stream.SSEBroker]).
func (ip *InProcess) Publish(_ context.Context, topic string, payload []byte) error {
	ip.mu.RLock()
	subs := make([]*ipSubscriber, 0, len(ip.topics[topic]))
	for _, s := range ip.topics[topic] {
		subs = append(subs, s)
	}
	ip.mu.RUnlock()

	for _, s := range subs {
		s.send(payload)
	}
	return nil
}

// Subscribe registers fn for topic. fn runs on a dedicated goroutine per
// subscriber with a bounded queue; delivery is in publish order. The returned
// cancel unregisters fn and stops the goroutine; safe to call multiple times.
func (ip *InProcess) Subscribe(topic string, fn func(payload []byte)) (cancel func(), err error) {
	send, stop := SubscriberQueue(fn, ip.queueDepth)

	ip.mu.Lock()
	s := &ipSubscriber{
		id:    ip.allocIDLocked(),
		topic: topic,
		send:  send,
		stop:  stop,
	}
	if ip.topics[topic] == nil {
		ip.topics[topic] = map[uint64]*ipSubscriber{}
	}
	ip.topics[topic][s.id] = s
	ip.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			ip.mu.Lock()
			delete(ip.topics[topic], s.id)
			ip.mu.Unlock()
			stop()
		})
	}, nil
}

// allocIDLocked returns the next subscriber id. Caller holds ip.mu.
func (ip *InProcess) allocIDLocked() uint64 {
	ip.nextSubID++
	return ip.nextSubID
}
