package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"slices"
	"strings"
	"testing"
)

type viewReplaceTestConn struct {
	exec func(context.Context, string) error
}

func (c *viewReplaceTestConn) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (c *viewReplaceTestConn) Driver() driver.Driver                        { return nil }
func (c *viewReplaceTestConn) Close() error                                 { return nil }
func (c *viewReplaceTestConn) Begin() (driver.Tx, error)                    { return c, nil }
func (c *viewReplaceTestConn) Commit() error                                { return nil }
func (c *viewReplaceTestConn) Rollback() error                              { return nil }
func (c *viewReplaceTestConn) Prepare(string) (driver.Stmt, error)          { return nil, errors.ErrUnsupported }

func (c *viewReplaceTestConn) ExecContext(ctx context.Context, stmt string, _ []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(0), c.exec(ctx, stmt)
}

func beginViewReplaceTestTx(t *testing.T, exec func(context.Context, string) error) *sql.Tx {
	t.Helper()
	db := sql.OpenDB(&viewReplaceTestConn{exec: exec})
	t.Cleanup(func() { _ = db.Close() })
	// Keep the transaction alive while a statement context is cancelled.
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func TestPGReplaceViewRollsBackDespiteACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var statements []string
	var name string
	tx := beginViewReplaceTestTx(t, func(ctx context.Context, stmt string) error {
		statements = append(statements, stmt)
		if sp, ok := strings.CutPrefix(stmt, "SAVEPOINT "); ok {
			name = sp
		}
		if strings.HasPrefix(stmt, "CREATE OR REPLACE VIEW ") {
			cancel()
		}
		if strings.HasPrefix(stmt, "ROLLBACK TO SAVEPOINT ") || strings.HasPrefix(stmt, "RELEASE SAVEPOINT ") {
			if ctx.Err() != nil {
				t.Errorf("%q ran under %v", stmt, ctx.Err())
			}
		}
		return ctx.Err()
	})
	if err := pgReplaceView(ctx, tx, View{Name: "rv", Select: "SELECT id FROM t"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("pgReplaceView returned %v, want context.Canceled", err)
	}
	want := []string{"SAVEPOINT " + name, "CREATE OR REPLACE VIEW rv AS SELECT id FROM t", "ROLLBACK TO SAVEPOINT " + name, "RELEASE SAVEPOINT " + name}
	if name == "" || !slices.Equal(statements, want) {
		t.Fatalf("statements = %q, want %q", statements, want)
	}
}

func TestPGReplaceViewDoesNotRecreateAfterSavepointError(t *testing.T) {
	for _, tc := range []struct {
		name         string
		failPrefix   string
		replaceFails bool
	}{
		{name: "setup", failPrefix: "SAVEPOINT "},
		{name: "release", failPrefix: "RELEASE SAVEPOINT "},
		{name: "rollback", failPrefix: "ROLLBACK TO SAVEPOINT ", replaceFails: true},
		{name: "release after rollback", failPrefix: "RELEASE SAVEPOINT ", replaceFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replaceErr := errors.New("cannot change name of view column")
			savepointErr := errors.New("savepoint operation failed")
			var savepoints int
			var injected bool
			tx := beginViewReplaceTestTx(t, func(_ context.Context, stmt string) error {
				if strings.HasPrefix(stmt, "SAVEPOINT ") {
					savepoints++
				}
				if !injected && strings.HasPrefix(stmt, tc.failPrefix) {
					injected = true
					return savepointErr
				}
				if tc.replaceFails && strings.HasPrefix(stmt, "CREATE OR REPLACE VIEW ") {
					return replaceErr
				}
				return nil
			})
			err := pgReplaceView(context.Background(), tx, View{Name: "rv", Select: "SELECT id FROM t"}, nil)
			if savepoints != 1 || !errors.Is(err, savepointErr) {
				t.Fatalf("pgReplaceView used %d savepoints and returned %v, want one attempt and %v", savepoints, err, savepointErr)
			}
			if tc.replaceFails && !errors.Is(err, replaceErr) {
				t.Fatalf("pgReplaceView returned %v, want the original replace error too", err)
			}
		})
	}
}

func TestPGReplaceViewRecreatesAfterCleanRollback(t *testing.T) {
	var names, statements []string
	tx := beginViewReplaceTestTx(t, func(_ context.Context, stmt string) error {
		statements = append(statements, stmt)
		if name, ok := strings.CutPrefix(stmt, "SAVEPOINT "); ok {
			names = append(names, name)
		}
		if strings.HasPrefix(stmt, "CREATE OR REPLACE VIEW ") {
			return errors.New("cannot change name of view column")
		}
		return nil
	})
	if err := pgReplaceView(context.Background(), tx, View{Name: "rv", Select: "SELECT id FROM t"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] == names[1] {
		t.Fatalf("savepoints = %q, want two distinct names", names)
	}
	want := []string{
		"SAVEPOINT " + names[0], "CREATE OR REPLACE VIEW rv AS SELECT id FROM t",
		"ROLLBACK TO SAVEPOINT " + names[0], "RELEASE SAVEPOINT " + names[0],
		"SAVEPOINT " + names[1], "DROP VIEW IF EXISTS rv", "CREATE VIEW rv AS SELECT id FROM t", "RELEASE SAVEPOINT " + names[1],
	}
	if !slices.Equal(statements, want) {
		t.Fatalf("statements = %q, want %q", statements, want)
	}
}
