package migrate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

func TestCreateDatabaseRaceSucceeds(t *testing.T) {
	for _, createErr := range []error{&pq.Error{Code: "42P04"}, fmt.Errorf("wrapped: %w", &pq.Error{Code: "42P04"}), &pq.Error{Code: "42501"}, errors.New("database already exists")} {
		t.Run(createErr.Error(), func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectPing()
			mock.ExpectQuery("SELECT EXISTS").WithArgs("other_app").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			mock.ExpectExec(`CREATE DATABASE "other_app"`).WillReturnError(createErr)
			created, err := ensureDatabaseOn(context.Background(), db, "other_app", "other_app")
			var pgErr *pq.Error
			duplicate := errors.As(createErr, &pgErr) && pgErr.Code == "42P04"
			if created || (err == nil) != duplicate {
				t.Fatalf("created=%v err=%v; duplicate=%v", created, err, duplicate)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
