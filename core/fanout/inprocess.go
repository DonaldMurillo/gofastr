package fanout

import (
	"context"
	"sync"
)

const defaultInProcessQueue = 256

// InProcess is an in-memory [Fanout]; two buses wired to one act as two replicas.
type InProcess struct {
	mu         sync.RWMutex
	nextSubID  uint64
	topics     map[string]map[uint64]*ipSubscriber
	queueDepth int
}

type ipSubscriber struct {
	id    uint64
	topic string
	send  func([]byte)
	stop  func()
}

// InProcessOption configures an [InProcess].
type InProcessOption func(*InProcess)

// WithInProcessQueue sets the per-subscriber queue depth; depth <= 0 keeps 256.
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

// Publish hands payload to every subscriber of topic without blocking.
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

// Subscribe registers fn for topic behind a [SubscriberQueue].
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

func (ip *InProcess) allocIDLocked() uint64 {
	ip.nextSubID++
	return ip.nextSubID
}
