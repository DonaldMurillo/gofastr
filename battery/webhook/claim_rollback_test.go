package webhook

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// A failed ROLLBACK on the SQLite claim path leaves no transaction
// active (SQLite 3.7.11 aborts pending reads instead of failing), so it
// is not fatal — but the queue and outbox claims log it, and silence hid
// it here (#659, first decision).
func TestClaimSqliteLogsFailedRollback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &SQLStore{
		db:       db,
		subTable: "webhook_subscribers",
		delTable: "webhook_deliveries",
		dialect:  "sqlite",
		codec:    NoopSecretCodec{},
	}

	var logs strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	mock.ExpectExec("BEGIN IMMEDIATE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT .* FROM webhook_deliveries").WillReturnError(errors.New("claim read failed"))
	mock.ExpectExec("ROLLBACK").WillReturnError(errors.New("rollback failed"))

	if _, err := s.ClaimDueDeliveries(context.Background(), time.Now(), 1, time.Second); err == nil {
		t.Fatal("the failed claim read was not returned")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "rolling back an abandoned claim") {
		t.Fatalf("a failed rollback was swallowed; logs: %q", logs.String())
	}
}
