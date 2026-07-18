package sqlite

import (
	"context"
	"database/sql"
	"testing"
)

func TestStatementsRollbackOnError(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*sql.DB, string) error
	}{
		{
			name: "autocommit",
			run: func(db *sql.DB, statement string) error {
				_, err := db.Exec(statement)
				return err
			},
		},
		{
			name: "explicit transaction",
			run: func(db *sql.DB, statement string) error {
				tx, err := db.BeginTx(context.Background(), nil)
				if err != nil {
					return err
				}
				if _, err = tx.Exec(statement); err == nil {
					_ = tx.Rollback()
					return nil
				}
				if _, continueErr := tx.Exec("INSERT INTO users VALUES (9, 'after@example.test')"); continueErr != nil {
					_ = tx.Rollback()
					t.Fatalf("continue transaction after statement error: %v", continueErr)
				}
				if commitErr := tx.Commit(); commitErr != nil {
					t.Fatalf("commit transaction after statement error: %v", commitErr)
				}
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, err := Open()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT UNIQUE)"); err != nil {
				t.Fatal(err)
			}
			err = test.run(db, `INSERT INTO users VALUES
				(1, 'duplicate@example.test'),
				(2, 'duplicate@example.test')`)
			if err == nil {
				t.Fatal("multi-row statement unexpectedly succeeded")
			}
			var partial int
			if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE id IN (1,2)").Scan(&partial); err != nil {
				t.Fatal(err)
			}
			if partial != 0 {
				t.Fatalf("failed statement retained %d partial rows", partial)
			}
		})
	}
}

func TestUniqueIndexRejectsExistingDuplicates(t *testing.T) {
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users VALUES
		(1, 'duplicate@example.test'),
		(2, 'duplicate@example.test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE UNIQUE INDEX users_email_unique ON users(email)"); err == nil {
		t.Fatal("unique index accepted existing duplicate values")
	}
	if _, err := db.Exec("CREATE INDEX users_email_unique ON users(email)"); err != nil {
		t.Fatalf("failed unique-index build left schema artifact behind: %v", err)
	}
}

func TestUniqueIndexEnforcesWrites(t *testing.T) {
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE UNIQUE INDEX users_email_unique ON users(email)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users VALUES (1, 'one@example.test')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users VALUES (2, 'one@example.test')"); err == nil {
		t.Fatal("unique index accepted duplicate insert")
	}
	if _, err := db.Exec("UPDATE users SET email='one@example.test' WHERE id=2"); err != nil {
		t.Fatalf("no matching row update should remain valid: %v", err)
	}
	if _, err := db.Exec("INSERT INTO users VALUES (2, 'two@example.test')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE users SET email='one@example.test' WHERE id=2"); err == nil {
		t.Fatal("unique index accepted duplicate update")
	}
}
