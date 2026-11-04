package a

import (
	"context"
	"database/sql"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"iolib"
)

const sweepInterval = 15 * time.Minute

// ---- 5ade7a35: the magic-link sweep held sweepMu across its DELETE ----

type MagicStore struct {
	db        *sql.DB
	sweepMu   sync.Mutex
	lastSweep time.Time
}

func (s *MagicStore) sweepExpiredOnCreate(ctx context.Context) { // want sweepExpiredOnCreate:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	now := time.Now()
	if now.Sub(s.lastSweep) < sweepInterval {
		return
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM magic WHERE expires_at < $1`, now.Unix()); err != nil { // want `iounderlock: ExecContext does I/O \(\(\*sql\.DB\)\.ExecContext\) while s\.sweepMu, which guards a throttle timestamp`
		return
	}
	s.lastSweep = now
}

// The 5ade7a35 spelling: claim the slot under the lock, DELETE outside.
func (s *MagicStore) sweepFixed(ctx context.Context) { // want sweepFixed:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	now := time.Now()
	s.sweepMu.Lock()
	if now.Sub(s.lastSweep) < sweepInterval {
		s.sweepMu.Unlock()
		return
	}
	s.lastSweep = now
	s.sweepMu.Unlock()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM magic WHERE expires_at < $1`, now.Unix()); err != nil { // quiet: the lock is released
		s.sweepMu.Lock()
		if s.lastSweep.Equal(now) {
			s.lastSweep = now.Add(time.Minute - sweepInterval)
		}
		s.sweepMu.Unlock()
	}
}

// Through an imported doesIO function.
func (s *MagicStore) sweepViaLib(ctx context.Context) { // want sweepViaLib:`doesIO\(iolib\.Purge → \(\*sql\.DB\)\.ExecContext, net=false\)`
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	if time.Since(s.lastSweep) < sweepInterval {
		return
	}
	_ = iolib.Purge(ctx, s.db) // want `iounderlock: Purge does I/O \(iolib\.Purge → \(\*sql\.DB\)\.ExecContext\) while s\.sweepMu`
	_ = iolib.Pure(1)
	s.lastSweep = time.Now()
}

// ---- ce72591b item 10: JWKS fetch and OIDC discovery under the mutex ----

type jwksCache struct {
	httpClient *http.Client
	ttl        time.Duration
	mu         sync.Mutex
	keys       []byte
	fetchedAt  time.Time
}

func (c *jwksCache) getKey(ctx context.Context, uri string) ([]byte, error) { // want getKey:`doesIO\(fetchLocked → \(\*http\.Client\)\.Do, net=true\)`
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys == nil || time.Since(c.fetchedAt) > c.ttl {
		if err := c.fetchLocked(ctx, uri); err != nil && c.keys == nil { // want `iounderlock: fetchLocked does I/O \(fetchLocked → \(\*http\.Client\)\.Do\) while c\.mu`
			return nil, err
		}
	}
	return c.keys, nil
}

func (c *jwksCache) fetchLocked(ctx context.Context, uri string) error { // want fetchLocked:`doesIO\(\(\*http\.Client\)\.Do, net=true\)`
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	c.keys = body
	c.fetchedAt = time.Now()
	return nil
}

// Discovery has no timestamp: network I/O under any held lock fires.
type provider struct {
	mu         sync.Mutex
	httpClient *http.Client
	discovery  []byte
}

func (p *provider) ensureDiscovery(ctx context.Context, uri string) ([]byte, error) { // want ensureDiscovery:`doesIO\(\(\*http\.Client\)\.Get, net=true\)`
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.discovery != nil {
		return p.discovery, nil
	}
	resp, err := p.httpClient.Get(uri) // want `iounderlock: Get does I/O \(\(\*http\.Client\)\.Get\) while p\.mu, which guards in-process state`
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return nil, nil
}

// The ce72591b spelling: a one-slot channel gate serializes the fetch, a
// waiter leaves at its own deadline, and the mutex guards only the copy.
type gatedCache struct {
	httpClient *http.Client
	gate       chan struct{}
	mu         sync.Mutex
	keys       []byte
	fetchedAt  time.Time
}

func (c *gatedCache) getKey(ctx context.Context, uri string) ([]byte, error) { // want getKey:`doesIO\(\(\*http\.Client\)\.Get, net=true\)`
	c.mu.Lock()
	fresh := c.keys != nil && time.Since(c.fetchedAt) < time.Hour
	keys := c.keys
	c.mu.Unlock()
	if fresh {
		return keys, nil
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return keys, ctx.Err()
	}
	defer func() { <-c.gate }()
	resp, err := c.httpClient.Get(uri) // quiet: no mutex held
	if err != nil {
		return keys, err
	}
	defer resp.Body.Close()
	c.mu.Lock()
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return keys, nil
}

// ---- silent postures ----

// A mutex that serializes the database work itself (a SQLite single
// writer, a role transition) and guards no throttle timestamp: quiet.
type writer struct {
	mu sync.Mutex
	db *sql.DB
}

func (w *writer) append(ctx context.Context, v string) error { // want append:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.db.ExecContext(ctx, `INSERT INTO log (v) VALUES (?)`, v)
	return err
}

// Near miss: the timestamp belongs to a different object than the
// mutex, so the lock is not a throttle over this I/O: quiet.
type job struct{ startedAt time.Time }

func (w *writer) record(ctx context.Context, j *job) error { // want record:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(j.startedAt) > time.Hour {
		return nil
	}
	_, err := w.db.ExecContext(ctx, `UPDATE jobs SET done = 1`)
	return err
}

// Near miss: the timestamp is read only after the I/O: quiet.
func (s *MagicStore) checkAfter(ctx context.Context) { // want checkAfter:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	_, _ = s.db.ExecContext(ctx, `SELECT 1`)
	if time.Since(s.lastSweep) > sweepInterval {
		s.lastSweep = time.Now()
	}
}

// A sync.Once body, a goroutine and a deferred call under the lock are
// not the linear region: quiet.
type lazy struct {
	mu        sync.Mutex
	once      sync.Once
	db        *sql.DB
	lastCheck time.Time
}

func (l *lazy) init(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.lastCheck) < time.Minute {
		return
	}
	l.once.Do(func() {
		_, _ = l.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS t (v TEXT)`)
	})
	go l.db.ExecContext(context.Background(), `DELETE FROM t`)
	defer l.db.Exec(`VACUUM`)
}

// An interface executor: the implementation is unknown (often in-memory):
// quiet.
type executor interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

type viaIface struct {
	mu        sync.Mutex
	ex        executor
	lastSweep time.Time
}

func (v *viaIface) sweep(ctx context.Context) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Since(v.lastSweep) < sweepInterval {
		return
	}
	_, _ = v.ex.ExecContext(ctx, `DELETE FROM t`)
}

// A dial under a held lock is network I/O.
type pool struct {
	mu    sync.Mutex
	conns []net.Conn
}

func (p *pool) grow(addr string) error { // want grow:`doesIO\(net\.Dial, net=true\)`
	p.mu.Lock()
	defer p.mu.Unlock()
	c, err := net.Dial("tcp", addr) // want `iounderlock: Dial does I/O \(net\.Dial\) while p\.mu`
	if err != nil {
		return err
	}
	p.conns = append(p.conns, c)
	return nil
}

// ---- R8-VA-04 / R8-VA-03 / R8-AZ8-05 ----

type otherStore struct {
	mu sync.Mutex
	n  int
}

// A throttle mutex taken second used to hide behind an unrelated lock
// taken first: every held mutex is judged, not just the first.
type dual struct {
	db        *sql.DB
	sweepMu   sync.Mutex
	lastSweep time.Time
}

func (d *dual) sweep(ctx context.Context, o *otherStore) { // want sweep:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	o.mu.Lock()
	defer o.mu.Unlock()
	d.sweepMu.Lock()
	defer d.sweepMu.Unlock()
	if time.Since(d.lastSweep) < sweepInterval {
		return
	}
	_, _ = d.db.ExecContext(ctx, `DELETE FROM t`) // want `iounderlock: ExecContext does I/O \(\(\*sql\.DB\)\.ExecContext\) while d\.sweepMu, which guards a throttle timestamp`
	d.lastSweep = time.Now()
}

func runNow(fn func()) { fn() }

// A closure passed straight to a call runs synchronously in the linear
// region: its I/O is under the lock.
type viaCallback struct {
	db        *sql.DB
	mu        sync.Mutex
	lastSweep time.Time
}

func (v *viaCallback) sweepCb(ctx context.Context) { // want sweepCb:`doesIO\(\(\*sql\.DB\)\.ExecContext, net=false\)`
	v.mu.Lock()
	defer v.mu.Unlock()
	if time.Since(v.lastSweep) < sweepInterval {
		return
	}
	runNow(func() {
		_, _ = v.db.ExecContext(ctx, `DELETE FROM t`) // want `iounderlock: ExecContext does I/O \(\(\*sql\.DB\)\.ExecContext\) while v\.mu, which guards a throttle timestamp`
	})
	v.lastSweep = time.Now()
}

// A closure handed to `go` or defer still runs outside the region.
func (v *viaCallback) sweepAsync(ctx context.Context) {
	v.mu.Lock()
	if time.Since(v.lastSweep) < sweepInterval {
		v.mu.Unlock()
		return
	}
	v.lastSweep = time.Now()
	v.mu.Unlock()
	go func() { _, _ = v.db.ExecContext(ctx, `DELETE FROM t`) }()
	defer func() { _, _ = v.db.Exec(`DELETE FROM t`) }()
}

// A package-level function literal is a handler-table entry: the same
// check applies outside any declaration.
var sweepPkg = func(ctx context.Context, s *MagicStore) {
	s.sweepMu.Lock()
	defer s.sweepMu.Unlock()
	if time.Since(s.lastSweep) < sweepInterval {
		return
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM t`) // want `iounderlock: ExecContext does I/O \(\(\*sql\.DB\)\.ExecContext\) while s\.sweepMu, which guards a throttle timestamp`
	s.lastSweep = time.Now()
}

// ---- launchers, storage and registration run it LATER ----

type dispatcher struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	client   *http.Client
	handlers []func()
}

// A closure handed to sync.WaitGroup.Go runs on its own goroutine: the
// launch itself is not I/O under the lock.
func (d *dispatcher) launch(u string) {
	d.mu.Lock()
	d.wg.Go(func() {
		_, _ = d.client.Get(u)
	})
	d.mu.Unlock()
}

// A closure merely stored with append runs when the caller chooses:
// not in this region.
func (d *dispatcher) store(u string) {
	d.mu.Lock()
	d.handlers = append(d.handlers, func() { _, _ = d.client.Get(u) })
	d.mu.Unlock()
}

// register only stores a handler on the mux: calling it under a lock
// does no I/O, and it must not carry a doesIO fact that would fire on
// every locked caller.
func (d *dispatcher) register(mux *http.ServeMux) {
	mux.HandleFunc("/x", func(w http.ResponseWriter, r *http.Request) {
		_, _ = d.client.Get("http://example.com")
	})
}

func (d *dispatcher) boot(mux *http.ServeMux) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.register(mux)
}
