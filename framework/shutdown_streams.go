package framework

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/stream"
)

const streamEndWait = 2 * time.Second

const shutdownCloseCode = 1001

var errDraining = errors.New("framework: server is shutting down")

// openStreams is the set of GET event streams and hijacked connections
// Shutdown ends when the drain starts.
type openStreams struct {
	mu     sync.Mutex
	next   uint64
	open   map[uint64]*longLived
	ending bool
}

type longLived struct {
	cancel context.CancelFunc
	set    *openStreams

	mu       sync.Mutex
	key      uint64
	ended    bool
	hijacked bool
	ws       *stream.WebSocketConn
	raw      net.Conn
}

// join puts l in the set; ok is false once the drain has started.
func (l *longLived) join() (added, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.key != 0 {
		return false, true
	}
	key, ok := l.set.add(l)
	l.key = key
	return ok, ok
}

func (l *longLived) attach(conn net.Conn) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ended {
		return false
	}
	l.hijacked = true
	l.raw = conn
	return true
}

func (l *longLived) leave() {
	l.mu.Lock()
	key := l.key
	l.key = 0
	l.mu.Unlock()
	if key != 0 {
		l.set.remove(key)
	}
}

func (l *longLived) setWS(c *stream.WebSocketConn) {
	l.mu.Lock()
	ended := l.ended
	if !ended {
		l.ws = c
	}
	l.mu.Unlock()
	if ended {
		_ = c.CloseWithStatus(shutdownCloseCode, "server shutting down")
	}
}

func (l *longLived) end() {
	l.cancel()
	l.mu.Lock()
	l.ended = true
	ws, raw := l.ws, l.raw
	l.mu.Unlock()
	switch {
	case ws != nil:
		_ = ws.CloseWithStatus(shutdownCloseCode, "server shutting down")
	case raw != nil:
		_ = raw.Close()
	}
}

func (l *longLived) forceClose() {
	l.mu.Lock()
	raw := l.raw
	l.mu.Unlock()
	if raw != nil {
		_ = raw.Close()
	}
}

func (s *openStreams) add(l *longLived) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ending {
		return 0, false
	}
	if s.open == nil {
		s.open = make(map[uint64]*longLived)
	}
	s.next++
	s.open[s.next] = l
	return s.next, true
}

func (s *openStreams) remove(key uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.open, key)
}

func (s *openStreams) endAll(ctx context.Context) {
	s.mu.Lock()
	s.ending = true
	ending := make([]*longLived, 0, len(s.open))
	for _, l := range s.open {
		ending = append(ending, l)
	}
	clear(s.open)
	s.mu.Unlock()
	if len(ending) == 0 {
		return
	}
	var wg sync.WaitGroup
	for _, l := range ending {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.end()
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	t := time.NewTimer(streamEndWait)
	defer t.Stop()
	select {
	case <-done:
		return
	case <-t.C:
	case <-ctx.Done():
	}
	for _, l := range ending {
		l.forceClose()
	}
}

func (a *App) endStreamsOnShutdown(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		l := &longLived{cancel: cancel, set: &a.streams}
		ctx = stream.WithUpgradeHook(ctx, l.setWS)
		sw := &streamWriter{ResponseWriter: w, life: l}
		defer func() {
			l.mu.Lock()
			hijacked := l.hijacked
			l.mu.Unlock()
			if !hijacked {
				l.leave()
			}
		}()
		next.ServeHTTP(sw, r.WithContext(ctx))
	})
}

type streamWriter struct {
	http.ResponseWriter
	life    *longLived
	checked bool
}

func (w *streamWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *streamWriter) noteHeaders() {
	if w.checked {
		return
	}
	w.checked = true
	mt, _, err := mime.ParseMediaType(w.Header().Get("Content-Type"))
	if err != nil || mt != "text/event-stream" {
		return
	}
	if _, ok := w.life.join(); !ok {
		w.life.cancel()
	}
}

func (w *streamWriter) WriteHeader(code int) {
	if code >= 200 {
		w.noteHeaders()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *streamWriter) Write(b []byte) (int, error) {
	w.noteHeaders()
	return w.ResponseWriter.Write(b)
}

func (w *streamWriter) ReadFrom(src io.Reader) (int64, error) {
	w.noteHeaders()
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(w.ResponseWriter, src)
}

func (w *streamWriter) FlushError() error {
	w.noteHeaders()
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *streamWriter) Flush() { _ = w.FlushError() }

func (w *streamWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	added, ok := w.life.join()
	if !ok {
		return nil, nil, errDraining
	}
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		if added {
			w.life.leave()
		}
		return nil, nil, err
	}
	sc := &streamConn{Conn: conn, release: w.life.leave}
	if !w.life.attach(sc) {
		_ = conn.Close()
		return nil, nil, errDraining
	}
	return sc, rw, nil
}

type streamConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *streamConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}

func (c *streamConn) NetConn() net.Conn { return c.Conn }
