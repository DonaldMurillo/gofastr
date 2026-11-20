package slowquery

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/lib/pq"
)

var errFake = errors.New("fake failure")

type fakeExec struct{}

func (fakeExec) QueryContext(context.Context, string, ...any) (*sql.Rows, error) { return nil, errFake }
func (fakeExec) QueryRowContext(context.Context, string, ...any) *sql.Row        { return nil }
func (fakeExec) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, errFake }
func (fakeExec) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)        { return nil, errFake }

func TestSlowLineCountsArgsAndCarriesTheError(t *testing.T) {
	const secret = "bound-secret"
	driverErr := &pq.Error{Code: "22P02", Message: "invalid integer: " + secret}
	for _, tc := range []struct {
		name string
		err  error
		want []string
	}{
		{"driver", driverErr, []string{`"err":"database_error"`, `"sqlstate":"22P02"`}},
		{"deadline", fmt.Errorf("dial: %w", context.DeadlineExceeded), []string{`"err":"deadline_exceeded"`}},
		{"canceled", context.Canceled, []string{`"err":"canceled"`}},
	} {
		inner := sql.OpenDB(failConnector{err: tc.err})
		t.Cleanup(func() { _ = inner.Close() })
		for _, kind := range []string{"exec", "query"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				var buf bytes.Buffer
				s := NewSlowQueryLogger(inner, 1, slog.New(slog.NewJSONHandler(&buf, nil)))
				ctx := context.Background()
				var err error
				if kind == "exec" {
					_, err = s.ExecContext(ctx, "SELECT $1::int", secret)
				} else {
					_, err = s.QueryContext(ctx, "SELECT $1::int", secret)
				}
				if !errors.Is(err, tc.err) {
					t.Fatalf("%s returned %v, want the original error", kind, err)
				}
				line := buf.String()
				if strings.Contains(line, secret) || strings.Contains(line, "dial") {
					t.Errorf("slow line leaked the error text or a bound value: %s", line)
				}
				for _, want := range append([]string{`"args":1`}, tc.want...) {
					if !strings.Contains(line, want) {
						t.Errorf("slow line missing %s: %s", want, line)
					}
				}
			})
		}
	}
}

func TestNilLoggerFallsBackToDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	for name, s := range map[string]*SlowQueryLogger{
		"nil accessor":      NewSlowQueryLoggerFn(fakeExec{}, 1, nil),
		"accessor gave nil": NewSlowQueryLoggerFn(fakeExec{}, 1, func() *slog.Logger { return nil }),
	} {
		buf.Reset()
		_, _ = s.QueryContext(context.Background(), "SELECT 1")
		if !strings.Contains(buf.String(), "slow query") {
			t.Fatalf("%s: slow line did not reach slog.Default", name)
		}
	}
}

func TestBeginTxForwardsToTheInnerExecutor(t *testing.T) {
	if _, err := NewSlowQueryLogger(fakeExec{}, 0, nil).BeginTx(context.Background(), nil); !errors.Is(err, errFake) {
		t.Fatalf("BeginTx = %v, want the inner executor's answer", err)
	}
}

type failConnector struct{ err error }

func (c failConnector) Connect(context.Context) (driver.Conn, error) { return nil, c.err }
func (failConnector) Driver() driver.Driver                          { return nil }

type fakeCodedError struct{ error }

func (fakeCodedError) Code() int { return 19 }

func TestQueryRowSlowLineCarriesTheDeferredError(t *testing.T) {
	const secret = "bound-secret"
	for _, tc := range []struct {
		name     string
		err      error
		wantCode string
	}{
		{"sqlstate", &pq.Error{Code: "22P02", Message: "invalid integer: " + secret}, `"sqlstate":"22P02"`},
		{"numeric", fakeCodedError{errors.New(secret)}, `"driver_code":19`},
		{"untyped", errors.New(secret), ""},
		{"invalid sqlstate", &pq.Error{Code: secret, Message: secret}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			inner := sql.OpenDB(failConnector{err: tc.err})
			t.Cleanup(func() { _ = inner.Close() })
			s := NewSlowQueryLogger(inner, 1, slog.New(slog.NewJSONHandler(&buf, nil)))
			row := s.QueryRowContext(context.Background(), "SELECT $1::int, '"+secret+"'", secret)
			if err := row.Err(); !errors.Is(err, tc.err) {
				t.Fatalf("row.Err() = %v, want the driver error", err)
			}
			if err := row.Scan(); !errors.Is(err, tc.err) {
				t.Fatalf("row.Scan() = %v, want the driver error", err)
			}
			line := buf.String()
			if strings.Contains(line, secret) {
				t.Errorf("slow query_row line exposed the value: %s", line)
			}
			if !strings.Contains(line, `"err":"database_error"`) {
				t.Errorf("slow query_row line missing the safe error kind: %s", line)
			}
			if tc.wantCode != "" && !strings.Contains(line, tc.wantCode) {
				t.Errorf("slow query_row line missing %s: %s", tc.wantCode, line)
			}
		})
	}
}
