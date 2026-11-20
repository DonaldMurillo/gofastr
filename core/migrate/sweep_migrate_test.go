package migrate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSweepMigrateGroupNameIsAtMost64(t *testing.T) {
	if err := ValidateGroupName(strings.Repeat("g", 64)); err != nil {
		t.Fatalf("a 64-byte group name was refused: %v", err)
	}
	if err := ValidateGroupName(strings.Repeat("g", 65)); err == nil {
		t.Fatal("a 65-byte group name was accepted")
	}
}

func TestSweepMigratePendingTiesBreakByGroup(t *testing.T) {
	m := New(nil)
	for _, g := range []string{"b", "a", ""} {
		if err := m.Register(Migration{Version: 1, Group: g, Up: "SELECT 1"}); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, mig := range pendingMigrations(m.selectedMigrations(nil), nil) {
		got = append(got, mig.Group)
	}
	if strings.Join(got, ",") != ",a,b" {
		t.Fatalf("pending order of one version in three groups = %q, want default, a, b", got)
	}
}

func TestSweepMigrateLockKeysAreFrozen(t *testing.T) {
	if AdvisoryLockKey != 6724469554113028193 || SeedAdvisoryLockKey != 7583194026157293042 {
		t.Fatalf("a lock key changed (%d, %d): replicas of two releases would no longer exclude each other", AdvisoryLockKey, SeedAdvisoryLockKey)
	}
}

func TestSweepMigrateHeartbeatOutlivesTheLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.db")
	holder, other := openSQLiteFile(t, path), openSQLiteFile(t, path)
	release, err := AcquireSQLiteLease(context.Background(), holder, "_sweep_lock", 2*time.Second, "sweep")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	time.Sleep(3500 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	rel, err := AcquireSQLiteLease(ctx, other, "_sweep_lock", 2*time.Second, "sweep")
	if err == nil {
		rel()
		t.Fatal("another process took a lease its holder was still renewing")
	}
	if ctx.Err() == nil {
		t.Fatalf("the contender failed before its context expired: %v", err)
	}
}

func TestSweepMigrateStatusBackfillsIntegrityColumnsOnly(t *testing.T) {
	m, db := newSQLiteMigrator(t)
	if _, err := db.Exec(`CREATE TABLE _migrations (version BIGINT NOT NULL PRIMARY KEY, name TEXT NOT NULL DEFAULT '', applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	mustReg(t, m, Migration{Group: "kb", Version: 1, Up: "CREATE TABLE sbi (id INT)"})
	if _, err := m.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	for col, want := range map[string]int{"checksum": 1, "dirty": 1, "group_name": 0} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('_migrations') WHERE name = ?`, col).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != want {
			t.Errorf("after Status, column %s present %d times, want %d", col, n, want)
		}
	}
}

func TestSweepMigrateEnsureRidesOutAStartingServer(t *testing.T) {
	shortPing(t)
	db, mock, _ := sqlmock.New(sqlmock.MonitorPingsOption(true))
	defer db.Close()
	mock.ExpectPing().WillReturnError(errors.New("starting up"))
	mock.ExpectPing()
	mock.ExpectQuery("pg_database").WithArgs("app").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	if _, err := ensureDatabaseOn(context.Background(), db, "app", "app"); err != nil {
		t.Fatalf("a server that answers on the second ping: %v", err)
	}
}

func TestSweepMigrateSQLiteNoTxTriggerIsOneExec(t *testing.T) {
	db := openSQLiteFile(t, filepath.Join(t.TempDir(), "trg.db"))
	m := New(db, WithDialect(DialectSQLite))
	if err := m.Register(Migration{Version: 1, NoTransaction: true, Up: "CREATE TABLE a (x INT); CREATE TABLE b (x INT);\n" +
		"CREATE TRIGGER t AFTER INSERT ON a BEGIN INSERT INTO b VALUES (1); INSERT INTO b VALUES (2); END;"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("a NoTransaction trigger migration on SQLite: %v", err)
	}
}

func TestSweepMigrateEditedDownIsNotDrift(t *testing.T) {
	db := openSQLiteFile(t, filepath.Join(t.TempDir(), "down.db"))
	mk := func(down string) *Migrator {
		m := New(db, WithDialect(DialectSQLite))
		if err := m.Register(Migration{Version: 1, Up: "CREATE TABLE a (x INT)", Down: down}); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if err := mk("DROP TABLE a").Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := mk("DROP TABLE IF EXISTS a").Up(context.Background()); err != nil {
		t.Fatalf("an edited Down tripped the drift check: %v", err)
	}
}
