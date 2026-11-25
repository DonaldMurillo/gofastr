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

func TestUpdateWithoutAssignmentsPanics(t *testing.T) {
	for _, tc := range []struct {
		name    string
		builder *UpdateBuilder
		want    string
	}{
		{"bare", Update("records"), "query: UPDATE records has no Set assignments"},
		{"with clauses", Update("notes").Where("id = $1", 42).Returning("id"), "query: UPDATE notes has no Set assignments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if got := recover(); got != tc.want {
					t.Fatalf("Build panic = %#v, want %q", got, tc.want)
				}
			}()
			tc.builder.Build()
		})
	}
}
