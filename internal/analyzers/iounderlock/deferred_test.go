package iounderlock_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
	"time"

	fixture "github.com/DonaldMurillo/gofastr/internal/analyzers/iounderlock/testdata/src/deferred"
)

type asyncContextKey struct{}

type lockProbeConnector struct{ inspect func(context.Context) }
type lockProbeDriver struct{ connector lockProbeConnector }
type lockProbeConnection struct{ connector lockProbeConnector }

func (connector lockProbeConnector) Connect(context.Context) (driver.Conn, error) {
	return lockProbeConnection{connector: connector}, nil
}

func (connector lockProbeConnector) Driver() driver.Driver {
	return lockProbeDriver{connector: connector}
}

func (probe lockProbeDriver) Open(string) (driver.Conn, error) {
	return probe.connector.Connect(context.Background())
}

func (connection lockProbeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unused prepare")
}

func (connection lockProbeConnection) Close() error { return nil }

func (connection lockProbeConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unused transaction")
}

func (connection lockProbeConnection) ExecContext(ctx context.Context, _ string, _ []driver.NamedValue) (driver.Result, error) {
	connection.connector.inspect(ctx)
	return driver.RowsAffected(1), nil
}

func TestSweepDeferredIOAfterUnlock(t *testing.T) {
	var mutex *sync.Mutex
	locked := make(chan bool, 1)
	database := sql.OpenDB(lockProbeConnector{inspect: func(ctx context.Context) {
		if ctx.Value(asyncContextKey{}) != nil {
			return
		}
		if mutex.TryLock() {
			mutex.Unlock()
			locked <- false
		} else {
			locked <- true
		}
	}})
	defer database.Close()
	sweep, mutex := fixture.NewDeferredIOFixture(database)
	sweep(context.WithValue(context.Background(), asyncContextKey{}, true))
	select {
	case held := <-locked:
		if held {
			t.Fatal("deferred database I/O ran while the throttle mutex was held")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("deferred database I/O never ran")
	}
}
