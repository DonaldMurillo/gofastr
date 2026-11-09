package semantic

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestChunkListingRejectsIncompleteRows(t *testing.T) {
	rowErr := errors.New("connection lost while reading")
	for _, tc := range []struct {
		name string
		rows *sqlmock.Rows
	}{
		{"scan", sqlmock.NewRows([]string{"id"}).AddRow("first").AddRow(nil)},
		{"next", sqlmock.NewRows([]string{"id"}).AddRow("first").AddRow("second").RowError(1, rowErr)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s, err := NewPgVector(db, PgVectorConfig{Dim: 3, Model: "test"})
			if err != nil {
				t.Fatal(err)
			}
			mock.ExpectQuery("SELECT id").WithArgs("d").WillReturnRows(tc.rows).RowsWillBeClosed()
			ids, err := s.ChunkIDsForDoc("d")
			if err == nil || ids != nil {
				t.Fatalf("incomplete listing = %v, %v; want nil and error", ids, err)
			}
			if tc.name == "next" && !errors.Is(err, rowErr) {
				t.Fatalf("listing error lost cause: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
