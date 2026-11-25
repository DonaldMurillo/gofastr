package query

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTransactionRollsBackOnPanic(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectRollback()
	func() {
		defer func() {
			if recover() != "callback failed" {
				t.Error("callback panic was not preserved")
			}
		}()
		_ = Transaction(context.Background(), db, func(_ *sql.Tx) error { panic("callback failed") })
	}()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRenumberPreservesQuotedIdentifiers(t *testing.T) {
	condition := `"column$7" = $1 AND "escaped""$8" = $2`
	want := `"column$7" = $3 AND "escaped""$8" = $4`
	if got := renumberPlaceholders(condition, 3); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestInsertDefaultValues(t *testing.T) {
	got, args := Insert("records").Returning("id").Build()
	if !strings.Contains(got, " DEFAULT VALUES RETURNING id") || len(args) != 0 {
		t.Fatalf("default insert SQL = %s, args=%v", got, args)
	}
}

func TestInsertMismatchedCountsPanics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		builder *InsertBuilder
		want    string
	}{
		{"columns only", Insert("users").Columns("a"), "query: INSERT users has mismatched Columns and Values counts (1 columns, 0 values)"},
		{"values only", Insert("users").Values(1), "query: INSERT users has mismatched Columns and Values counts (0 columns, 1 values)"},
		{"fewer values", Insert("users").Columns("a", "b").Values(1), "query: INSERT users has mismatched Columns and Values counts (2 columns, 1 values)"},
		{"more values", Insert("users").Columns("a").Values(1, 2).Returning("id"), "query: INSERT users has mismatched Columns and Values counts (1 columns, 2 values)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sql string
			defer func() {
				if got := recover(); got != tc.want {
					t.Fatalf("Build panic = %#v, want %q (SQL = %q)", got, tc.want, sql)
				}
			}()
			sql, _ = tc.builder.Build()
		})
	}
}
