package sqlite

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"
)

func TestReturningQueriesUseWriteLock(t *testing.T) {
	for _, test := range []struct {
		name  string
		query func(*conn) (driver.Rows, error)
	}{
		{
			name: "direct",
			query: func(c *conn) (driver.Rows, error) {
				return c.QueryContext(context.Background(),
					"INSERT INTO items (id, value) VALUES (1, 'direct') RETURNING id", nil)
			},
		},
		{
			name: "prepared",
			query: func(c *conn) (driver.Rows, error) {
				stmt, err := c.PrepareContext(context.Background(),
					"INSERT INTO items (id, value) VALUES (2, 'prepared') RETURNING id")
				if err != nil {
					return nil, err
				}
				defer stmt.Close()
				return stmt.(driver.StmtQueryContext).QueryContext(context.Background(), nil)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine, err := newMemEngine()
			if err != nil {
				t.Fatal(err)
			}
			shared := newSharedEngine(engine)
			c := &conn{shared: shared}
			if _, err := c.ExecContext(context.Background(),
				"CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)", nil); err != nil {
				t.Fatal(err)
			}

			shared.mu.RLock()
			done := make(chan error, 1)
			go func() {
				rows, err := test.query(c)
				if rows != nil {
					_ = rows.Close()
				}
				done <- err
			}()
			select {
			case err := <-done:
				shared.mu.RUnlock()
				if err != nil {
					t.Fatalf("query returned under read lock with error: %v", err)
				}
				t.Fatal("mutating RETURNING query ran while another reader held the lock")
			case <-time.After(50 * time.Millisecond):
			}
			shared.mu.RUnlock()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("query after write lock release: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("mutating RETURNING query stayed blocked after write lock release")
			}
		})
	}
}

func TestConflictTargetKeepsOtherUniqueConstraints(t *testing.T) {
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		email TEXT UNIQUE,
		handle TEXT UNIQUE
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users (id,email,handle) VALUES (1,'one@example.test','one')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id,email,handle)
		VALUES (1,'two@example.test','two')
		ON CONFLICT(email) DO NOTHING`); err == nil {
		t.Fatal("non-target primary-key conflict succeeded")
	}
	var email string
	if err := db.QueryRow("SELECT email FROM users WHERE id=1").Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "one@example.test" {
		t.Fatalf("primary-key identity was overwritten with %q", email)
	}
}

func TestConflictUpdateMaintainsConstraintsAndIndex(t *testing.T) {
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		email TEXT UNIQUE,
		handle TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE INDEX users_handle_idx ON users(handle)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO users (id,email,handle) VALUES (1,'one@example.test','old')"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users (id,email,handle)
		VALUES (2,'one@example.test','ignored')
		ON CONFLICT(email) DO UPDATE SET handle=NULL`); err == nil {
		t.Fatal("conflict update accepted NULL for NOT NULL column")
	}
	if _, err := db.Exec(`INSERT INTO users (id,email,handle)
		VALUES (2,'one@example.test','ignored')
		ON CONFLICT(email) DO UPDATE SET handle='new'`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE handle='old'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("stale indexed key returned %d rows", count)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM users WHERE handle='new'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new indexed key returned %d rows, want 1", count)
	}
}

func TestParserRejectsTrailingSQL(t *testing.T) {
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT UNIQUE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO items VALUES (1,'old')"); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO items VALUES (2,'old')
		ON CONFLICT(value) DO UPDATE SET value='changed' WHERE false`)
	if err == nil {
		t.Fatal("unsupported trailing upsert predicate was silently ignored")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unexpected") {
		t.Fatalf("trailing SQL error = %q, want unexpected token", err)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM items WHERE id=1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != "old" {
		t.Fatalf("unsupported tail mutated value to %q", value)
	}
}

func TestReturningUnknownColumnFailsBeforeMutation(t *testing.T) {
	db, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("INSERT INTO items VALUES (1,'inserted') RETURNING missing")
	if rows != nil {
		_ = rows.Close()
	}
	if err == nil {
		t.Fatal("unknown RETURNING column succeeded")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("invalid RETURNING mutated %d rows", count)
	}
}
