// Package plumbingish pins the name/shape postures that keep the
// interface leg off framework-internal plumbing, each reduced from a
// real oracle site: the database/sql trio (framework/db.Executor,
// taken as a parameter by crud's eager loader, migrate, and the
// durable queue — framework/crud/eager.go:183, core/migrate/integrity.
// go:143, battery/queue/durable_scheduler.go:721), the os/exec +
// io.Closer teardown family (RunningChild wraps an exec.Cmd —
// framework/processmodule_supervisor.go:932/934/938; schedulerStartStop
// narrows the queues' Close — framework/app.go:2709), and accessor-
// shaped getters (multiplex c.ID — control/multiplex/multiplex.go:209;
// acp agent.Info — core/acp/server.go:447). All on dispatch loops, all
// quiet.
package plumbingish

import "context"

// Store mirrors framework/db.Executor: the database/sql spelling, so
// *sql.DB and *sql.Tx satisfy it.
type Store interface {
	QueryContext(ctx context.Context, query string, args ...any) (any, error)
	ExecContext(ctx context.Context, query string, args ...any) (int64, error)
}

func queryLoop(ctx context.Context, db Store, q string) {
	for {
		_, _ = db.QueryContext(ctx, q) // quiet: the sql trio, data access not events
	}
}

func execOnce(ctx context.Context, db Store, q string) {
	go func() {
		_, _ = db.ExecContext(ctx, q) // quiet
	}()
}

type Child interface {
	CloseStdin()
	Wait() error
	Kill()
	Signal(sig int) error
	Close() error
}

func teardownLoop(c Child) {
	for range 3 {
		c.CloseStdin()  // quiet: os/exec teardown family
		_ = c.Wait()    // quiet
		c.Kill()        // quiet
		_ = c.Signal(9) // quiet
		_ = c.Close()   // quiet: io.Closer
	}
}

// Stopper mirrors schedulerStartStop (framework/app.go:2673).
type Stopper interface {
	Start(ctx context.Context)
	Close() error
}

func drainOne(q Stopper) {
	done := make(chan error, 1)
	go func() { done <- q.Close() }() // quiet
	<-done
}

// Session mirrors the multiplex connection / acp agent: a getter.
type Session interface {
	ID() string
}

func turnLoop(s Session) {
	go func() {
		_ = s.ID() // quiet: accessor-shaped, a value query not an event
	}()
}
