package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/event"
)

// TestMetricsEndpointCarriesFrameworkCollectorsWithNoBattery pins the
// documented zero-config surface on the endpoint a started app serves:
// WithDB + WithMetrics exposes the db_pool gauges, and WithOutbox the
// outbox pending/dead-letter gauges, at /metrics even when no battery or
// plugin ever called app.Metrics(). Start used to mount the bare store,
// so both collectors were missing and an alert on outbox_dead_letter
// had no series to fire on.
func TestMetricsEndpointCarriesFrameworkCollectorsWithNoBattery(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	app, cleanup := startApp(t, NewApp(WithDB(db), WithMetrics(),
		WithOutbox(),
		WithOutboxConsumer("witness", event.EntityCreated,
			func(context.Context, event.Event) error { return nil }),
	))
	defer cleanup()
	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d", rec.Code)
	}
	for _, want := range []string{
		"# TYPE db_pool_open_connections gauge",
		"# TYPE outbox_pending gauge",
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("/metrics lacks %q, which the framework documents for this configuration:\n%s", want, rec.Body.String())
		}
	}
}
