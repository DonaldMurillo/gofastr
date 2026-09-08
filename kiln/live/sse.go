package live

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Event is one notification published when a journal entry is applied.
// Subscribers learn that something changed; they can pull current state
// (e.g. specific entity data or the world summary) via separate calls.
type Event struct {
	EntryID string `json:"entry_id"`
	Kind    string `json:"kind"`
	Op      string `json:"op,omitempty"`
	// Summary is a glanceable, human-readable digest of the entry's
	// payload (e.g. "name=posts fields=3" for add_entity). Empty for
	// kinds that don't have a useful one-liner. Computed in Apply.
	Summary string `json:"summary,omitempty"`
}

// Broadcaster fan-outs Events to subscribed channels. Slow consumers
// drop events rather than blocking the broadcaster.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewBroadcaster returns an empty Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: map[chan Event]struct{}{}}
}

// Subscribe registers a channel and returns it along with an unsubscribe
// function. The channel is closed by unsubscribe; callers must call it.
func (b *Broadcaster) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	once := sync.Once{}
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
			close(ch)
		})
	}
}

// Send delivers e to every subscribed channel. Non-blocking: a subscriber
// whose buffer is full will miss this event.
func (b *Broadcaster) Send(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// sseSeatsPerPrincipal is the per-principal cap on concurrent ServeSSE
// streams: the same number as core/stream's defaultSeatsPerPrincipal and
// framework/crud's defaultEventStreamSeats, so the three seat policies in
// the tree read as one number. Each seat is a goroutine plus a buffered
// channel, so an uncounted stream surface is a memory/fd exhaustion
// target for a single caller.
const sseSeatsPerPrincipal = 16

// sseSeatRegistry counts resident ServeSSE streams per principal. This
// transport is unauthenticated, so the principal is the TCP peer's host
// (every anonymous connection from one origin shares one bucket — the
// same posture framework/crud's event stream takes for anonymous
// callers). The policy is refuse-at-connect: the principal at its cap
// gets a 429 and holds nothing.
type sseSeatRegistry struct {
	mu    sync.Mutex
	seats map[string]int
}

// admit seats one stream for principal, reporting whether it was
// admitted. Over-cap callers are refused, not queued.
func (r *sseSeatRegistry) admit(principal string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seats == nil {
		r.seats = map[string]int{}
	}
	if r.seats[principal] >= sseSeatsPerPrincipal {
		return false
	}
	r.seats[principal]++
	return true
}

// release frees a seat when its stream ends; idempotent per admission.
func (r *sseSeatRegistry) release(principal string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := r.seats[principal]; n <= 1 {
		delete(r.seats, principal)
	} else {
		r.seats[principal] = n - 1
	}
}

// sseSeatPrincipal derives the seat-bucket identity from the TCP peer.
// The port is deliberately dropped: one caller's dials all share a host,
// and counting by full RemoteAddr would make the cap per-connection
// rather than per-caller.
func sseSeatPrincipal(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.Trim(host, "[]")
}

// ServeSSE is a stand-alone HTTP handler that streams events as
// Server-Sent Events. Mount at e.g. "/.kiln/events".
func (l *Live) ServeSSE(w http.ResponseWriter, r *http.Request) {
	// Seat admission before anything is written: one principal holds at
	// most sseSeatsPerPrincipal resident streams (a goroutine plus a
	// buffered channel each), the 17th is answered 429 at connect. The
	// seat is held until the handler returns.
	principal := sseSeatPrincipal(r)
	if !l.sseSeats.admit(principal) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many event streams", http.StatusTooManyRequests)
		return
	}
	defer l.sseSeats.release(principal)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := l.Subscribe()
	defer unsub()

	// Send a hello event so the client knows the stream is open even
	// before the first real edit lands.
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			// Scrub the kind before it becomes an SSE field name.
			// Event.Kind is journal-derived, so an entry whose kind
			// carries a CR or LF closes the "event:" line and lets the
			// rest of the value write its own fields -- an extra data:
			// frame, or a whole synthetic event the client dispatches as
			// if the server had sent it. The data field is JSON-encoded
			// and cannot do this; the kind was interpolated raw.
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", scrubSSEField(ev.Kind), data)
			flusher.Flush()
		}
	}
}

// scrubSSEField strips the bytes that terminate a line or a frame in the
// SSE wire format, so a value can never introduce a field of its own.
// CR and LF end a field; a NUL is not meaningful in the format and has no
// business in an event name either.
func scrubSSEField(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', 0:
			return -1
		}
		return r
	}, s)
}
