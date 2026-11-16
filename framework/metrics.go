package framework

import (
	"database/sql"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// Metrics returns the app's metrics store, or nil without WithMetrics. It
// attaches the DB pool and outbox collectors; batteries add their own
// with RegisterCollector.
func (a *App) Metrics() *middleware.Metrics {
	m := a.metrics
	if m == nil {
		return nil
	}
	if a.DB != nil {
		db := a.DB
		m.RegisterCollector("db_pool", func(w io.Writer) {
			writeDBPoolMetrics(w, db.Stats())
		})
	}
	if a.outbox != nil {
		m.RegisterCollector("outbox", a.outbox.MetricsCollector())
	}
	return m
}

func writeDBPoolMetrics(w io.Writer, s sql.DBStats) {
	fmt.Fprintf(w, "# HELP db_pool_open_connections Open connections in the pool.\n")
	fmt.Fprintf(w, "# TYPE db_pool_open_connections gauge\n")
	fmt.Fprintf(w, "db_pool_open_connections %d\n", s.OpenConnections)

	fmt.Fprintf(w, "# HELP db_pool_in_use_connections Connections currently in use.\n")
	fmt.Fprintf(w, "# TYPE db_pool_in_use_connections gauge\n")
	fmt.Fprintf(w, "db_pool_in_use_connections %d\n", s.InUse)

	fmt.Fprintf(w, "# HELP db_pool_idle_connections Idle connections waiting in the pool.\n")
	fmt.Fprintf(w, "# TYPE db_pool_idle_connections gauge\n")
	fmt.Fprintf(w, "db_pool_idle_connections %d\n", s.Idle)

	fmt.Fprintf(w, "# HELP db_pool_wait_count_total Total number of connections waited for.\n")
	fmt.Fprintf(w, "# TYPE db_pool_wait_count_total counter\n")
	fmt.Fprintf(w, "db_pool_wait_count_total %d\n", s.WaitCount)

	fmt.Fprintf(w, "# HELP db_pool_wait_duration_seconds_total Total time blocked waiting for a connection.\n")
	fmt.Fprintf(w, "# TYPE db_pool_wait_duration_seconds_total counter\n")
	fmt.Fprintf(w, "db_pool_wait_duration_seconds_total %s\n", seconds(s.WaitDuration))
}

func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}
