package queue

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A queue table at Postgres's 63-byte cap still gets three scheduler
// tables of its own. The names were the table name plus a suffix, which
// Postgres cut back to the queue table's name, so CREATE TABLE IF NOT
// EXISTS made nothing and every schedule write hit the queue table. At 45
// bytes the tables kept distinct cuts but both occurrence indexes cut to
// the occurrences table's name and were skipped.
func TestPG_SchedulerLongTableName(t *testing.T) {
	for _, n := range []int{45, 63} {
		t.Run(fmt.Sprintf("len%d", n), func(t *testing.T) {
			db := pgtest.DB(t)
			table := strings.Repeat("q", n)
			q, err := NewDBQueue(db, WithTable(table))
			if err != nil {
				t.Fatal(err)
			}
			sched, err := NewDurableScheduler(q, DurableSchedulerConfig{OwnerID: "long", LeaseDuration: time.Minute})
			if err != nil {
				t.Fatalf("new scheduler: %v", err)
			}
			base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
			if err := sched.Every("digest", time.Minute).Job("digest", nil).RegisterAt(base); err != nil {
				t.Fatalf("register: %v", err)
			}
			if err := sched.RunOnce(context.Background(), base.Add(time.Minute+time.Microsecond)); err != nil {
				t.Fatalf("run: %v", err)
			}
			if jobs := pendingJobs(t, q); len(jobs) != 1 {
				t.Fatalf("pending jobs = %d, want 1", len(jobs))
			}
			var tables, indexes int
			if err := db.QueryRow(`SELECT COUNT(*) FROM pg_tables
				WHERE schemaname = current_schema() AND tablename LIKE 'qqq%'`).Scan(&tables); err != nil {
				t.Fatal(err)
			}
			if tables != 4 {
				t.Fatalf("tables = %d, want the queue table and three scheduler tables", tables)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM pg_indexes
				WHERE schemaname = current_schema() AND tablename = $1`,
				pgCut(table+"_scheduler_occurrences")).Scan(&indexes); err != nil {
				t.Fatal(err)
			}
			// The primary key, the UNIQUE(schedule_id, scheduled_tick)
			// constraint and the two retention indexes.
			if indexes != 4 {
				t.Fatalf("occurrence table indexes = %d, want 4", indexes)
			}
		})
	}
}

// Where Postgres's cut of a scheduler name collides with nothing, the
// name stays the cut, the relation earlier releases created; a table
// whose cut only an index shared keeps it too, so its rows stay put.
// Every length gets five distinct names that are not the queue table's.
func TestSchedulerNamesOnPostgres(t *testing.T) {
	names := func(n int) (string, []string) {
		q := &DBQueue{table: strings.Repeat("q", n), dialect: dialectPostgres}
		out := make([]string, len(schedulerSuffixes))
		for i, s := range schedulerSuffixes {
			out[i] = q.schedulerName(s)
		}
		return q.table, out
	}
	table, got := names(30)
	for i, s := range schedulerSuffixes {
		if want := pgCut(table + s); got[i] != want {
			t.Errorf("len 30 %s = %q, want the cut %q", s, got[i], want)
		}
	}
	table, got = names(45)
	for i, s := range schedulerSuffixes[:3] {
		if want := pgCut(table + s); got[i] != want {
			t.Errorf("len 45 table %s = %q, want the cut %q", s, got[i], want)
		}
	}
	for i, s := range schedulerSuffixes[3:] {
		if cut := pgCut(table + s); got[3+i] == cut {
			t.Errorf("len 45 index %s kept the cut %q the occurrences table holds", s, cut)
		}
	}
	for n := 1; n <= 80; n++ {
		table, got := names(n)
		seen := map[string]bool{pgCut(table): true}
		for i, name := range got {
			if len(name) > maxIdentLen {
				t.Errorf("len %d %s = %d bytes", n, schedulerSuffixes[i], len(name))
			}
			if seen[name] {
				t.Errorf("len %d %s = %q, already taken", n, schedulerSuffixes[i], name)
			}
			seen[name] = true
		}
	}
	q := &DBQueue{table: strings.Repeat("q", 63), dialect: dialectSQLite}
	if got := q.schedulerName("_scheduler_leases"); got != q.table+"_scheduler_leases" {
		t.Errorf("SQLite name = %q, want the full name", got)
	}
}
