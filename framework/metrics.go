package framework

import (
	"database/sql"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// Metrics returns the app's Prometheus metrics store, or nil when
// WithMetrics was not used.
//
// Batteries and plugins call this from Init to attach subsystem collectors
// (queue depth, outbox lag, webhook failures, …) to the single /metrics
// surface via RegisterCollector, the same store the HTTP middleware and
// /metrics endpoint already use. There is no second handler to mount and no
// cross-package import cycle: a battery reaches *Metrics through the App it
// already holds, and writes its own Prometheus text lines from a collector.
//
// The framework-level DB-pool collector is wired here, idempotently, so an
// app built with WithDB + WithMetrics exposes sql.DBStats gauges/counters
// with no extra configuration: the first caller that takes the handle (a
// battery's Init in a real app) attaches it. RegisterCollector replaces by
// name, so repeated calls do not duplicate output.
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
	// The transactional outbox is framework-owned (WithOutbox), so its
	// per-consumer pending/dead-letter counts surface automatically, no
	// battery or wiring needed beyond enabling the outbox + metrics.
	if a.outbox != nil {
		m.RegisterCollector("outbox", a.outbox.MetricsCollector())
	}
	return m
}

// writeDBPoolMetrics renders the DB connection-pool gauges and wait
// counters from a sql.DBStats snapshot, in Prometheus text exposition
// format. Called once per /metrics scrape; cheap (one Stats() call).
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

// seconds formats a duration as a compact number of seconds for a Prometheus
// counter sample (e.g. 0.00123456). -1 precision picks the shortest exact
// representation.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}
