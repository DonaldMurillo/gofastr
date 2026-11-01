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

// streamEndWait bounds how long Shutdown waits for open WebSockets to
// trade close frames before it moves on. Each close is already bounded
// by its own WSConfig.CloseTimeout (1s by default); this keeps a large
// configured one from holding the drain.
const streamEndWait = 2 * time.Second

// shutdownCloseCode is the WebSocket status Shutdown closes with: 1001,
// "going away", the code RFC 6455 gives a server that is going down.
const shutdownCloseCode = 1001

// errDraining is the Hijack error once the drain has started: a
// connection taken over then would outlive the Shutdown that already
// ended the others.
var errDraining = errors.New("framework: server is shutting down")

// openStreams is the set of long-lived responses the server is serving:
// GET event streams and hijacked connections (WebSockets). Neither ends
// on its own, and Shutdown ends both when the drain starts.
//
// A request joins the set by what its handler does, never by what the
// client asked for: a GET whose response goes out as text/event-stream,
// or a handler that hijacks the connection. A client's Accept header
// says nothing either way (curl -N sends */* and gets a stream; a JSON
// GET may list text/event-stream with q=0).
//
//   - An event stream never goes idle, so http.Server.Shutdown would
//     wait on each one until the drain deadline (AppConfig.
//     ShutdownTimeout, 15s by default) and then cut it. Its request
//     runs on a context this set cancels; every stream loop already
//     returns when its request context ends, the way it does when the
//     client goes away. A POST answered as a stream (MCP's streamable
//     HTTP) is one call's reply: it drains the normal way.
//   - A hijacked connection is outside Server.Shutdown, which neither
//     waits for it nor closes it: without this it outlives Shutdown and
//     its handler keeps working against batteries that have stopped. A
//     core/stream WebSocket gets a 1001 close frame
//     (stream.WithUpgradeHook); a connection hijacked by hand is closed.
//
// Clients reconnect, to another replica or to the restarted process.
type openStreams struct {
	mu     sync.Mutex
	next   uint64
	open   map[uint64]*longLived
	ending bool
}

// longLived is one GET request that may turn into a stream: the cancel
// for its context and, once its handler takes the connection over, what
// to close.
type longLived struct {
	cancel context.CancelFunc
	set    *openStreams

	mu       sync.Mutex
	key      uint64 // 0 while not in the set
	hijacked bool
	ws       *stream.WebSocketConn
	raw      net.Conn
}

// join puts l in the set. It reports false once the drain has started,
// and the caller ends the stream at once.
func (l *longLived) join() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.key != 0 {
		return true
	}
	key, ok := l.set.add(l)
	l.key = key
	return ok
}

// leave takes l out of the set.
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
	l.ws = c
	l.mu.Unlock()
}

// end cancels the request context, then closes the connection the
// handler took over, if any.
func (l *longLived) end() {
	l.cancel()
	l.mu.Lock()
	ws, raw := l.ws, l.raw
	l.mu.Unlock()
	switch {
	case ws != nil:
		_ = ws.CloseWithStatus(shutdownCloseCode, "server shutting down")
	case raw != nil:
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

// endAll ends every open stream, concurrently, and waits up to
// streamEndWait (or ctx) for the WebSocket close handshakes. The ends
// run outside the lock: a close fires the connection's OnClose hooks,
// and the set stays a leaf lock.
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
	case <-t.C:
	case <-ctx.Done():
	}
}

// endStreamsOnShutdown gives each GET a context Shutdown can cancel and
// a writer that notices when the handler turns the response into a
// stream. Other methods pass through as they are.
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
			// A hijacked connection stays in the set until it closes
			// (streamConn.Close): a WebSocket handler commonly hands the
			// connection to a goroutine and returns.
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

// streamWriter watches one GET response: headers going out as
// text/event-stream put the request in the open set, and so does a
// hijack.
type streamWriter struct {
	http.ResponseWriter
	life    *longLived
	checked bool
}

// Unwrap lets http.ResponseController reach the writer underneath.
func (w *streamWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// noteHeaders runs once, as the status line goes out.
func (w *streamWriter) noteHeaders() {
	if w.checked {
		return
	}
	w.checked = true
	mt, _, err := mime.ParseMediaType(w.Header().Get("Content-Type"))
	if err != nil || mt != "text/event-stream" {
		return
	}
	if !w.life.join() {
		// The drain has already ended the others: end this one too.
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

// ReadFrom keeps the sendfile path of the writer underneath.
func (w *streamWriter) ReadFrom(src io.Reader) (int64, error) {
	w.noteHeaders()
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(w.ResponseWriter, src)
}

// Flush keeps the Flusher the writer underneath has. A flush sends the
// headers, so it is where a stream that has written nothing yet shows.
func (w *streamWriter) Flush() {
	w.noteHeaders()
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

// Hijack takes the connection over and records it, or refuses once the
// drain has started. The returned connection takes the stream out of
// the open set when it closes.
func (w *streamWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if !w.life.join() {
		return nil, nil, errDraining
	}
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		w.life.leave()
		return nil, nil, err
	}
	sc := &streamConn{Conn: conn, release: w.life.leave}
	w.life.mu.Lock()
	w.life.hijacked = true
	w.life.raw = sc
	w.life.mu.Unlock()
	return sc, rw, nil
}

// streamConn is a hijacked connection that leaves the open set when it
// closes.
type streamConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *streamConn) Close() error {
	c.once.Do(c.release)
	return c.Conn.Close()
}

// NetConn returns the connection underneath, the way tls.Conn does.
func (c *streamConn) NetConn() net.Conn { return c.Conn }
