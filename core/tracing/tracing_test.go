package tracing_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/DonaldMurillo/gofastr/core/tracing"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	prev := otel.GetTracerProvider()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	return rec
}

func attr(s sdktrace.ReadOnlySpan, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes() {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func spanNamed(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var names []string
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
		names = append(names, s.Name())
	}
	t.Fatalf("no span %q; have %q", name, names)
	return nil
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := tracing.OpenDB("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSQLSpanPerStatement(t *testing.T) {
	rec := recordSpans(t)
	db := openDB(t)
	ctx, parent := otel.Tracer("test").Start(context.Background(), "request")
	if _, err := db.ExecContext(ctx, `INSERT INTO notes (body) VALUES (?)`, "hello"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM notes WHERE id > 0`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("count = %d, %v", n, err)
	}
	parent.End()

	ins := spanNamed(t, rec, "INSERT")
	if ins.SpanKind() != trace.SpanKindClient {
		t.Errorf("kind = %v, want client", ins.SpanKind())
	}
	if ins.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Error("the INSERT span is not a child of the request span")
	}
	if v, _ := attr(ins, "db.system.name"); v.AsString() != "sqlite" {
		t.Errorf("db.system.name = %q", v.AsString())
	}
	if v, _ := attr(ins, "db.query.text"); v.AsString() != `INSERT INTO notes (body) VALUES (?)` {
		t.Errorf("db.query.text = %q", v.AsString())
	}
	sel := spanNamed(t, rec, "SELECT")
	if v, _ := attr(sel, "db.query.text"); v.AsString() != `SELECT count(*) FROM notes WHERE id > ?` {
		t.Errorf("literal kept in db.query.text: %q", v.AsString())
	}
}

func TestSQLSpanStripsLiterals(t *testing.T) {
	rec := recordSpans(t)
	db := openDB(t)
	if _, err := db.Exec(`INSERT INTO notes (id, body) VALUES (42, 'it''s secret')`); err != nil {
		t.Fatal(err)
	}
	v, _ := attr(spanNamed(t, rec, "INSERT"), "db.query.text")
	if got := v.AsString(); strings.Contains(got, "secret") || strings.Contains(got, "42") {
		t.Errorf("db.query.text = %q keeps a literal", got)
	}
}

func TestSQLSpanMarksErrors(t *testing.T) {
	rec := recordSpans(t)
	db := openDB(t)
	if _, err := db.Exec(`SELECT * FROM missing`); err == nil {
		t.Fatal("query on a missing table succeeded")
	}
	if s := spanNamed(t, rec, "SELECT"); s.Status().Code != codes.Error {
		t.Errorf("status = %v, want error", s.Status())
	}
}

func TestSQLTransactionsWork(t *testing.T) {
	rec := recordSpans(t)
	db := openDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO notes (body) VALUES (?)`, "a"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows after rollback = %d, %v", n, err)
	}
	spanNamed(t, rec, "ROLLBACK")

	stmt, err := db.Prepare(`INSERT INTO notes (body) VALUES (?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if _, err := stmt.Exec("b"); err != nil {
		t.Fatal(err)
	}
	if got := len(rec.Ended()); got < 4 {
		t.Errorf("spans = %d, want the prepared INSERT counted", got)
	}
}

func TestTransportSpanAndNoTraceHeader(t *testing.T) {
	rec := recordSpans(t)
	var gotTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := &http.Client{Transport: tracing.Transport(nil)}
	ctx, parent := otel.Tracer("test").Start(context.Background(), "job")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/hook?token=s3cret", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	parent.End()

	s := spanNamed(t, rec, "HTTP POST")
	if s.SpanKind() != trace.SpanKindClient {
		t.Errorf("kind = %v", s.SpanKind())
	}
	if s.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Error("the client span is not a child of the job span")
	}
	if v, _ := attr(s, "http.response.status_code"); v.AsInt64() != http.StatusBadGateway {
		t.Errorf("status attr = %v", v)
	}
	if s.Status().Code != codes.Error {
		t.Errorf("a 502 is not marked an error")
	}
	if v, _ := attr(s, "url.full"); strings.Contains(v.AsString(), "s3cret") || !strings.HasSuffix(v.AsString(), "/hook") {
		t.Errorf("url.full = %q", v.AsString())
	}
	if gotTraceparent != "" {
		t.Errorf("traceparent sent without PropagateTraceContext: %q", gotTraceparent)
	}
}

func TestTransportPropagatesWhenAsked(t *testing.T) {
	recordSpans(t)
	var gotTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTraceparent = r.Header.Get("traceparent")
	}))
	defer srv.Close()
	client := &http.Client{Transport: tracing.Transport(nil, tracing.PropagateTraceContext())}
	ctx, parent := otel.Tracer("test").Start(context.Background(), "request")
	defer parent.End()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !strings.Contains(gotTraceparent, parent.SpanContext().TraceID().String()) {
		t.Errorf("traceparent = %q, want the trace id", gotTraceparent)
	}
	if req.Header.Get("traceparent") != "" {
		t.Error("the caller's request was mutated")
	}
}

func TestTransportMarksDialErrors(t *testing.T) {
	rec := recordSpans(t)
	client := &http.Client{Transport: tracing.Transport(nil)}
	if _, err := client.Get("http://127.0.0.1:1/"); err == nil {
		t.Fatal("dial to port 1 succeeded")
	}
	if s := spanNamed(t, rec, "HTTP GET"); s.Status().Code != codes.Error {
		t.Errorf("status = %v, want error", s.Status())
	}
}
