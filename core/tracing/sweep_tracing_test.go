package tracing

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/DonaldMurillo/gofastr/internal/sqlredact"
)

type stubRT struct {
	status int
	closed int
}

func (s *stubRT) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: s.status, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
}

func (s *stubRT) CloseIdleConnections() { s.closed++ }

func sweepAttr(s sdktrace.ReadOnlySpan, key string) string {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value.Emit()
		}
	}
	return ""
}

func onlySpan(t *testing.T, rec *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	if n := len(rec.Ended()); n != 1 {
		t.Fatalf("%d spans ended, want 1", n)
	}
	return rec.Ended()[0]
}

func TestSweepTracingProviderInstalledAfterWrap(t *testing.T) {
	recordSpans(t)
	rt := Transport(&stubRT{status: 200})
	rec := recordSpans(t)
	req, _ := http.NewRequest(http.MethodGet, "http://example.test/", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	onlySpan(t, rec)
}

func TestSweepTracingErrorTypeNotMessage(t *testing.T) {
	rec := recordSpans(t)
	c := &tracedConn{}
	c.end(context.Background(), "SELECT 1", time.Now(), errors.New("duplicate key value 'alice@example.com'"))
	s := onlySpan(t, rec)
	if s.Status().Code != codes.Error || s.Status().Description != "" {
		t.Fatalf("status = %+v, want error with no description", s.Status())
	}
	if got := sweepAttr(s, "error.type"); got != "*errors.errorString" {
		t.Fatalf("error.type = %q", got)
	}
}

func TestSweepTracingURLDropsSecretParts(t *testing.T) {
	rec := recordSpans(t)
	u, _ := url.Parse("http://user:pw@example.test/hook?token=s3cret#frag")
	if _, err := Transport(&stubRT{status: 200}).RoundTrip(&http.Request{Method: http.MethodGet, URL: u, Header: http.Header{}}); err != nil {
		t.Fatal(err)
	}
	if got := sweepAttr(onlySpan(t, rec), "url.full"); got != "http://example.test/hook" {
		t.Fatalf("url.full = %q", got)
	}
}

func TestSweepTracingStatusMarksError(t *testing.T) {
	for _, tc := range []struct {
		status int
		failed bool
	}{{200, false}, {399, false}, {400, true}, {404, true}, {503, true}} {
		rec := recordSpans(t)
		req, _ := http.NewRequest(http.MethodGet, "http://example.test/", nil)
		if _, err := Transport(&stubRT{status: tc.status}).RoundTrip(req); err != nil {
			t.Fatal(err)
		}
		if got := onlySpan(t, rec).Status().Code == codes.Error; got != tc.failed {
			t.Errorf("status %d: span failed = %v, want %v", tc.status, got, tc.failed)
		}
	}
}

func TestSweepTracingCloseIdleConnectionsForwards(t *testing.T) {
	base := &stubRT{status: 200}
	(&http.Client{Transport: Transport(base)}).CloseIdleConnections()
	if base.closed != 1 {
		t.Fatalf("base saw %d CloseIdleConnections calls", base.closed)
	}
}

type skipConn struct{ legacyConn }

func (skipConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, driver.ErrSkip
}

func TestSweepTracingErrSkipRecordsNoSpan(t *testing.T) {
	rec := recordSpans(t)
	if _, err := (&tracedConn{inner: &skipConn{}}).ExecContext(context.Background(), "UPDATE t SET a = 1", nil); !errors.Is(err, driver.ErrSkip) {
		t.Fatalf("err = %v, want driver.ErrSkip", err)
	}
	if n := len(rec.Ended()); n != 0 {
		t.Fatalf("%d spans for an ErrSkip answer", n)
	}
}

func TestSweepTracingCommitUnderBeginContext(t *testing.T) {
	rec := recordSpans(t)
	ctx, parent := otel.Tracer("test").Start(context.Background(), "request")
	c := &tracedConn{inner: &legacyConn{}}
	tx, err := c.BeginTx(ctx, driver.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	parent.End()
	for _, s := range rec.Ended() {
		if s.Name() == "COMMIT" && s.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("COMMIT is not under the context BeginTx was given")
		}
	}
}

func TestSweepTracingLegacyDriverRefusesNamedArgs(t *testing.T) {
	inner := &legacyConn{}
	_, err := (&tracedConn{inner: inner}).ExecContext(context.Background(), "UPDATE t SET a = :a", []driver.NamedValue{{Name: "a", Ordinal: 1, Value: 1}})
	if err == nil || inner.calls != 0 {
		t.Fatalf("err = %v, calls = %d; a named argument reached a legacy driver", err, inner.calls)
	}
}

func TestSweepTracingQueryTextCapIs4KiB(t *testing.T) {
	q := "INSERT INTO t (a) VALUES " + strings.Repeat("(?), ", 5000)
	if got := len(queryText(q, sqlredact.Postgres)); got > 4096 || got < 4000 {
		t.Fatalf("len(db.query.text) = %d, want the first 4 KiB", got)
	}
}

func TestSweepTracingDriverNameToSystem(t *testing.T) {
	for in, want := range map[string]string{
		"sqlite3": "sqlite", "sqlite": "sqlite", "postgres": "postgresql", "pgx": "postgresql",
		"pgx/v5": "postgresql", "mysql": "mysql", "clickhouse": "clickhouse",
	} {
		if got := dbSystem(in); got != want {
			t.Errorf("dbSystem(%q) = %q, want %q", in, got, want)
		}
	}
}
