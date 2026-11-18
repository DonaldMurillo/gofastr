package slowquery

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

var errFake = errors.New("fake failure")

type fakeExec struct{}

func (fakeExec) QueryContext(context.Context, string, ...any) (*sql.Rows, error) { return nil, errFake }
func (fakeExec) QueryRowContext(context.Context, string, ...any) *sql.Row        { return nil }
func (fakeExec) ExecContext(context.Context, string, ...any) (sql.Result, error) { return nil, errFake }
func (fakeExec) BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)        { return nil, errFake }

func TestSlowLineCountsArgsAndCarriesTheError(t *testing.T) {
	var buf bytes.Buffer
	s := NewSlowQueryLogger(fakeExec{}, 1, slog.New(slog.NewJSONHandler(&buf, nil)))
	_, _ = s.ExecContext(context.Background(), "UPDATE t SET v = $1", "bound-secret")
	line := buf.String()
	if strings.Contains(line, "bound-secret") || !strings.Contains(line, `"args":1`) {
		t.Fatalf("slow line must count bound args, never print them: %s", line)
	}
	if !strings.Contains(line, errFake.Error()) {
		t.Fatalf("slow line dropped the statement's error: %s", line)
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
