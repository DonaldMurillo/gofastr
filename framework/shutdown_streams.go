package framework

import (
	"context"
	"net/http"
	"strings"
	"sync"
)

// openStreams is the set of event-stream requests the server is serving.
// An SSE stream never goes idle, so http.Server.Shutdown would wait on
// each one until the drain deadline (AppConfig.ShutdownTimeout, 15s by
// default) and then cut it. Shutdown ends them instead when the drain
// starts: each request runs on a context this set can cancel, and every
// stream loop already returns when its request context ends, the way it
// does when the client goes away. The client's EventSource reconnects,
// to another replica or to the restarted process.
//
// WebSockets are not in the set: a hijacked connection is outside
// Server.Shutdown's drain, so it does not hold it.
type openStreams struct {
	mu     sync.Mutex
	next   uint64
	open   map[uint64]context.CancelFunc
	ending bool
}

// add records cancel and returns its key. Once ending, it cancels at
// once instead, so a stream that starts during the drain ends too.
func (s *openStreams) add(cancel context.CancelFunc) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ending {
		return 0, false
	}
	if s.open == nil {
		s.open = make(map[uint64]context.CancelFunc)
	}
	s.next++
	s.open[s.next] = cancel
	return s.next, true
}

func (s *openStreams) remove(key uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.open, key)
}

// endAll cancels every open stream's context. The cancel funcs run
// outside the lock: each is context.CancelFunc, which runs no caller
// code, but the set stays a leaf lock either way.
func (s *openStreams) endAll() {
	s.mu.Lock()
	s.ending = true
	cancels := make([]context.CancelFunc, 0, len(s.open))
	for _, c := range s.open {
		cancels = append(cancels, c)
	}
	clear(s.open)
	s.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// isEventStreamRequest reports a GET that asks for text/event-stream,
// which is what an EventSource sends. A POST that may answer with a
// stream (MCP's streamable HTTP) is request-bounded work and drains the
// normal way.
func isEventStreamRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	for _, v := range r.Header.Values("Accept") {
		if strings.Contains(strings.ToLower(v), "text/event-stream") {
			return true
		}
	}
	return false
}

// endStreamsOnShutdown runs each event-stream request on a context the
// app cancels when Shutdown starts. Other requests pass through as they
// are.
func (a *App) endStreamsOnShutdown(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isEventStreamRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		key, added := a.streams.add(cancel)
		if !added {
			cancel()
		} else {
			defer a.streams.remove(key)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
