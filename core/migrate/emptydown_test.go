package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A migration with no Down section has nothing to undo its Up. Down ran
// the empty string, which succeeds, then deleted the tracking row: the
// schema kept the Up's changes while the version read as pending, so
// the next Up ran it again (a CREATE failed, a backfill doubled).
// migrations.md promises that migrate down fails for such a version.
func TestDownRefusesMigrationWithoutDown(t *testing.T) {
	for _, c := range []struct {
		name   string
		down   string
		noTx   bool
		latest bool   // the blank Down is the newest applied version
		group  string // both migrations' group
		want   string // the refusal names the migration this way
	}{
		{name: "no down, newest", down: "", latest: true},
		{name: "whitespace down, newest", down: " \n\t", latest: true},
		{name: "no down, no transaction", down: "", noTx: true, latest: true},
		{name: "no down, behind one that has one", down: ""},
		{name: "no down, in a group", down: "", latest: true, group: "knowledge", want: `(add_b) in group "knowledge"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, db := newSQLiteMigrator(t)
			ctx := context.Background()
			blank := Migration{Version: 2, Name: "add_b", Group: c.group, Up: "CREATE TABLE b (id INTEGER);", Down: c.down, NoTransaction: c.noTx}
			other := Migration{Version: 1, Name: "add_a", Group: c.group, Up: "CREATE TABLE a (id INTEGER);", Down: "DROP TABLE a;"}
			if !c.latest {
				blank.Version, other.Version = 1, 2
			}
			for _, mig := range []Migration{other, blank} {
				if err := m.Register(mig); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.Up(ctx); err != nil {
				t.Fatal(err)
			}

			want := c.want
			if want == "" {
				want = "migration 2 (add_b): "
				if !c.latest {
					want = "migration 1 (add_b): "
				}
			}
			err := m.Down(ctx, 2)
			if !errors.Is(err, ErrNoDown) || !strings.Contains(err.Error(), want) {
				t.Fatalf("Down = %v, want ErrNoDown naming %q", err, want)
			}
			// Refused before anything ran: both rows stay, clean, and
			// the migration with a Down was not rolled back either.
			for _, v := range []uint64{1, 2} {
				if !upRowRecorded(t, m, v) {
					t.Errorf("version %d's tracking row is gone", v)
				}
			}
			var dirty int
			if err := db.QueryRow("SELECT COUNT(*) FROM _migrations WHERE dirty").Scan(&dirty); err != nil {
				t.Fatal(err)
			}
			if dirty != 0 {
				t.Errorf("%d rows left dirty", dirty)
			}
			if !tableExists(t, db, "a") || !tableExists(t, db, "b") {
				t.Error("a table was dropped")
			}
			// Up stays a no-op: nothing reads as pending.
			if err := m.Up(ctx); err != nil {
				t.Errorf("Up after the refusal: %v", err)
			}
			// Only the versions being rolled back need a Down.
			if !c.latest {
				if err := m.Down(ctx, 1); err != nil {
					t.Fatalf("Down(1) over the version with a Down: %v", err)
				}
				if upRowRecorded(t, m, 2) || tableExists(t, db, "a") {
					t.Error("Down(1) did not roll back version 2")
				}
			}
		})
	}
}
