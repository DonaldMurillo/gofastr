package a

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"sync"
)

type Store interface {
	Begin(ctx context.Context, key string) (bool, error)
	Finish(ctx context.Context, key string, body []byte) error
}

// Idempotency before c0d6e99e: the claim is released after the handler,
// outside a defer, so a panicking handler strands it.
func Idempotency(store Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.Begin(r.Context(), "k"); err != nil {
			return
		}
		next.ServeHTTP(w, r)
		finish := func(body []byte) { _ = store.Finish(context.Background(), "k", body) }
		finish(nil) // want `deferreset: finish undoes Begin \(line 20\) after next\.ServeHTTP outside a defer`
	})
}

// The c0d6e99e spelling: a deferred recover releases and re-panics.
func IdempotencyFixed(store Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := store.Begin(r.Context(), "k"); err != nil {
			return
		}
		finish := func(body []byte) { _ = store.Finish(context.Background(), "k", body) }
		defer func() {
			if v := recover(); v != nil {
				finish(nil)
				panic(v)
			}
		}()
		next.ServeHTTP(w, r)
		finish([]byte("ok")) // quiet: the deferred recover covers the panic path
	})
}

type timeoutWriter struct {
	mu         sync.Mutex
	w          http.ResponseWriter
	hijacking  bool
	streaming  bool
	hijackDone *sync.Cond
}

// Hijack before 39de7827: the flag is cleared after the Hijack, so a
// Hijack that panics leaves it set and expire waits forever.
func (tw *timeoutWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	tw.mu.Lock()
	tw.hijacking = true
	tw.mu.Unlock()
	conn, rw, err := http.NewResponseController(tw.w).Hijack()
	tw.mu.Lock()
	tw.hijacking = false // want `deferreset: tw\.hijacking undoes tw\.hijacking \(line 59\) after http\.NewResponseController\(tw\.w\)\.Hijack outside a defer`
	tw.hijackDone.Broadcast()
	tw.mu.Unlock()
	return conn, rw, err
}

// The 39de7827 spelling: cleared in a defer.
func (tw *timeoutWriter) hijackFixed() (net.Conn, *bufio.ReadWriter, error) {
	tw.mu.Lock()
	tw.hijacking = true
	tw.mu.Unlock()
	defer func() {
		tw.mu.Lock()
		tw.hijacking = false
		tw.hijackDone.Broadcast()
		tw.mu.Unlock()
	}()
	return http.NewResponseController(tw.w).Hijack()
}

// A host callback through a func-typed field is a panic point too.
type runner struct {
	busy   bool
	OnStep func(int) error
}

func (r *runner) step(i int) error {
	r.busy = true
	err := r.OnStep(i)
	r.busy = false // want `deferreset: r\.busy undoes r\.busy \(line 90\) after r\.OnStep outside a defer`
	return err
}

// And through a func-typed parameter, with a claim verb.
type pool struct{}

func (p *pool) acquire(ctx context.Context) error { return nil }
func (p *pool) release()                          {}

func (p *pool) with(ctx context.Context, fn func() error) error {
	if err := p.acquire(ctx); err != nil {
		return err
	}
	err := fn()
	p.release() // want `deferreset: release undoes acquire \(line 103\) after fn outside a defer`
	return err
}

// ---- silent postures ----

// Nothing that can panic between set and reset: the function's own code.
func (r *runner) ownCode(i int) int {
	r.busy = true
	n := i * 2
	r.busy = false
	return n
}

// A local closure is the function's own code, not a callback.
func (r *runner) localClosure(i int) {
	work := func() { _ = i }
	r.busy = true
	work()
	r.busy = false
}

// A local holding a named function value is the function's own code.
func double(i int) int { return i * 2 }

func (r *runner) namedValue(i int) int {
	step := double
	r.busy = true
	n := step(i)
	r.busy = false
	return n
}

// A latch set before the handler and never reset is not this shape.
type latch struct{ started bool }

func (l *latch) serve(next http.Handler, w http.ResponseWriter, r *http.Request) {
	l.started = true
	next.ServeHTTP(w, r)
}

// A reset before the panic point closes the set; a later call is outside.
func (r *runner) resetFirst(i int) error {
	r.busy = true
	r.busy = false
	return r.OnStep(i)
}

// A different field reset after the call does not undo this one.
type twoFlags struct {
	a, b   bool
	OnStep func()
}

func (t *twoFlags) run() {
	t.a = true
	t.OnStep()
	t.b = false
}

// The deferred spelling with a verb.
func (p *pool) withFixed(ctx context.Context, fn func() error) error {
	if err := p.acquire(ctx); err != nil {
		return err
	}
	defer p.release()
	return fn()
}
