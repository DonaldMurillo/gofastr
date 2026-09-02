// Package broker is a NOVEL instantiation of the shape: a message
// broker worker (no such code exists in this repo) draining a channel
// and invoking subscriber callbacks from a map, with and without a
// recover net.
package broker

type Subscriber func(topic string, payload []byte)

type Broker struct {
	subs map[string]Subscriber
	in   chan delivery
}

type delivery struct {
	topic   string
	payload []byte
}

// runBad is the shape: a select-over-channel wait loop calling a
// map-derived callback with no recover.
func (b *Broker) runBad() {
	for {
		select {
		case d := <-b.in:
			sub, ok := b.subs[d.topic]
			if ok {
				sub(d.topic, d.payload) // want `recovercallback: sub is invoked with no recover in scope`
			}
		}
	}
}

// runGood wraps the same call in the recover every dispatch loop
// needs: a panicking subscriber is contained, not fatal.
func (b *Broker) runGood() {
	for d := range b.in {
		sub, ok := b.subs[d.topic]
		if !ok {
			continue
		}
		b.deliver(sub, d)
	}
}

func (b *Broker) deliver(sub Subscriber, d delivery) {
	defer func() {
		if rec := recover(); rec != nil {
			_ = rec
		}
	}()
	sub(d.topic, d.payload)
}

// syncOnly is quiet: no loop, no goroutine — a panic here unwinds to a
// caller that can see it.
func (b *Broker) syncOnly(d delivery) {
	if sub, ok := b.subs[d.topic]; ok {
		sub(d.topic, d.payload)
	}
}
