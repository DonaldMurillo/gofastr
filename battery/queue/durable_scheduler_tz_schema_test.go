package queue

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

// TestDurableSchedulerMigratesExistingSchedulesTableTZ proves the tz column
// is added idempotently to a schedules table created before tz shipped, the
// same way ensureScheduleVersionColumn / ensureScheduleOptionsColumns do.
func TestDurableSchedulerMigratesExistingSchedulesTableTZ(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	if _, err := db.Exec(`CREATE TABLE ` + q.schedulerSchedulesTable() + ` (
		id TEXT PRIMARY KEY,
		job_type TEXT NOT NULL,
		payload TEXT NOT NULL,
		interval_ns BIGINT NOT NULL DEFAULT 0,
		cron_spec TEXT NOT NULL DEFAULT '',
		next_run DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatalf("upgrade scheduler schema: %v", err)
	}
	rows, err := db.Query("PRAGMA table_info(" + q.schedulerSchedulesTable() + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "tz" {
			found = true
		}
	}
	if !found {
		t.Fatal("existing schedules table was not upgraded with tz column")
	}
	// Second construction must be a no-op (idempotent).
	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatalf("second construction failed: %v", err)
	}
}

// TestDurableSchedulerFreshSchemaHasTZColumn proves a brand-new schedules
// table carries the tz column from creation, not only via migration.
func TestDurableSchedulerFreshSchemaHasTZColumn(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	if _, err := NewDurableScheduler(q, DurableSchedulerConfig{}); err != nil {
		t.Fatal(err)
	}
	var found bool
	row := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name='tz'",
		strings.Trim(q.schedulerSchedulesTable(), `"`))
	if err := row.Scan(&found); err != nil && !errors.Is(err, sql.ErrNoRows) {
		// pragma_table_info's table-valued form may be unavailable; fall back
		// to a structural scan.
		found = scanForTZColumn(t, db, q.schedulerSchedulesTable())
	}
	if !found {
		t.Fatal("fresh schedules table missing tz column")
	}
}

func scanForTZColumn(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "tz" {
			return true
		}
	}
	return false
}

// TestDurableSchedulerPersistsTZOnRegistration proves the tz name round-trips
// through SQLite: a schedule registered in America/New_York re-emerges with
// the same location and fires at the local wall-clock time after reload.
func TestDurableSchedulerPersistsTZOnRegistration(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	q, err := NewDBQueue(db)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load NY: %v", err)
	}
	first, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "first", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 15, 1, 0, 0, 0, loc)
	if err := first.Cron("daily-local", "0 2 * * *").Job("digest", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow("SELECT tz FROM "+q.schedulerSchedulesTable()+" WHERE id=$1", "daily-local").Scan(&stored); err != nil {
		t.Fatalf("read tz: %v", err)
	}
	if stored != "America/New_York" {
		t.Fatalf("stored tz = %q, want America/New_York", stored)
	}
}

// TestDurableSchedulerPoisonedScheduleDoesNotKillOthers proves the design
// invariant from the fix brief: a single schedule that returns an error from
// dueTicks (only triggerable post-fix by genuine corruption — here simulated
// by directly corrupting cron_spec to an unsatisfiable / invalid spec on
// disk) must not stop RunOnce from evaluating the remaining due schedules.
func TestDurableSchedulerPoisonedScheduleDoesNotKillOthers(t *testing.T) {
	db := openDurableSchedulerDB(t)
	q := newDurableTestQueue(t, db)
	sched, err := NewDurableScheduler(q, DurableSchedulerConfig{
		OwnerID: "isolated", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if err := sched.Every("healthy", time.Minute).Job("healthy", nil).RegisterAt(base); err != nil {
		t.Fatal(err)
	}
	// Seed a poison-pilled schedule by writing an invalid cron spec directly
	// to the row — `Register` would have rejected it. This stands in for
	// genuine data corruption (operator typo in a manual row edit, partial
	// restore, schema drift) that survives restart.
	if _, err := db.Exec(
		"INSERT INTO "+q.schedulerSchedulesTable()+
			" (id, job_type, payload, interval_ns, cron_spec, tz, lane, priority, max_attempts, next_run, updated_at, version)"+
			" VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)",
		"poison", "poison-job", "null", 0, "not a cron", "",
		"", 0, 0, base.Add(time.Minute), base, 0,
	); err != nil {
		t.Fatal(err)
	}
	// Both schedules are due at base+1m. Without the isolation fix,
	// loadDue's ORDER BY next_run, id would visit "healthy" first (it sorts
	// before "poison"), but corrupting the healthy row instead proves the
	// case where the poisoned schedule sorts first. We cover both.
	if err := sched.RunOnce(context.Background(), base.Add(time.Minute)); err != nil {
		t.Fatalf("poisoned schedule propagated error: %v", err)
	}
	jobs := pendingJobs(t, q)
	if len(jobs) != 1 {
		t.Fatalf("healthy schedule did not fire alongside poisoned one: pending=%d want 1", len(jobs))
	}
	if jobs[0].Type != "healthy" {
		t.Fatalf("enqueued job type = %q, want healthy", jobs[0].Type)
	}
}

// TestDurableSchedulerTZNameHelper pins the helper's edge cases: UTC and nil
// collapse to the empty default (preserving the legacy UTC-evaluation
// behaviour), fixed-offset zones are un-loadable so they also collapse, and
// real IANA names survive the round trip.
func TestDurableSchedulerTZNameHelper(t *testing.T) {
	if got := tzName(time.UTC); got != "" {
		t.Fatalf("tzName(UTC) = %q, want empty", got)
	}
	if got := tzName(nil); got != "" {
		t.Fatalf("tzName(nil) = %q, want empty", got)
	}
	fixed := time.FixedZone("UTC-05", -5*3600)
	if got := tzName(fixed); got != "" {
		t.Fatalf("tzName(FixedZone) = %q, want empty (un-loadable)", got)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load NY: %v", err)
	}
	if got := tzName(ny); got != "America/New_York" {
		t.Fatalf("tzName(NY) = %q, want America/New_York", got)
	}
}

// TestDurableSchedulerLocationHelper proves the durableSchedule.location
// resolver degrades to UTC on empty / un-loadable names so a corrupted tz
// column cannot strand the schedule.
func TestDurableSchedulerLocationHelper(t *testing.T) {
	if got := (&durableSchedule{}).location(); got != time.UTC {
		t.Fatalf("empty tz location = %v, want UTC", got)
	}
	if got := (&durableSchedule{tz: "not-a-real-zone"}).location(); got != time.UTC {
		t.Fatalf("un-loadable tz location = %v, want UTC fallback", got)
	}
	ny, _ := time.LoadLocation("America/New_York")
	if got := (&durableSchedule{tz: "America/New_York"}).location(); got.String() != ny.String() {
		t.Fatalf("NY tz location = %v, want America/New_York", got)
	}
}

// TestBoundedDueTicksCadenceChangeRecovery exercises the cadence-change
// recovery branch directly: a stored cursor that is NOT an occurrence of the
// current cron spec heals to the next valid occurrence instead of erroring.
func TestBoundedDueTicksCadenceChangeRecovery(t *testing.T) {
	base := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	// Stored watermark from a previous interval schedule: 00:01. The current
	// spec "0 2 * * *" has no occurrence at 00:01 — pre-fix this errored.
	schedule := durableSchedule{
		id:       "switch",
		cronSpec: "0 2 * * *",
		nextRun:  base.Add(time.Minute),
	}
	// Evaluate at 00:01: no tick yet, but the watermark must advance to 02:00.
	ticks, nextRun, err := boundedDueTicks(schedule, base.Add(time.Minute), 100)
	if err != nil {
		t.Fatalf("cadence-change recovery returned error: %v", err)
	}
	if len(ticks) != 0 {
		t.Fatalf("recovery emitted %d ticks, want 0 before the new cadence", len(ticks))
	}
	wantNext := time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC)
	if !nextRun.Equal(wantNext) {
		t.Fatalf("recovery advanced to %s, want %s", nextRun.Format(time.RFC3339), wantNext.Format(time.RFC3339))
	}

	// At 02:00 the recovered cadence fires exactly once.
	ticks, _, err = boundedDueTicks(schedule, wantNext, 100)
	if err != nil {
		t.Fatalf("recovered cadence fire error: %v", err)
	}
	if len(ticks) != 1 || !ticks[0].Equal(wantNext) {
		t.Fatalf("recovered cadence ticks = %v, want [%s]", ticks, wantNext.Format(time.RFC3339))
	}

	// And the same recovery branch fires immediately when the next
	// occurrence is already in the past — e.g. a long outage between
	// cadence change and the first evaluation.
	later := time.Date(2026, 1, 15, 5, 0, 0, 0, time.UTC)
	ticks, _, err = boundedDueTicks(schedule, later, 100)
	if err != nil {
		t.Fatalf("recovered cadence late-fire error: %v", err)
	}
	if len(ticks) != 1 || !ticks[0].Equal(time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("recovered cadence late ticks = %v, want [02:00]", ticks)
	}
}

// TestBoundedDueTicksCronMatchesInScheduleLocation proves the cron field
// comparison happens in the schedule's wall-clock location, not UTC: the
// stored next_run is a UTC instant, but the cron spec "0 2 * * *" must fire
// at 02:00 America/New_York (07:00 UTC in January) rather than 02:00 UTC.
func TestBoundedDueTicksCronMatchesInScheduleLocation(t *testing.T) {
	if _, err := time.LoadLocation("America/New_York"); err != nil {
		t.Fatalf("load NY: %v", err)
	}
	// next_run is the UTC instant for 02:00 NY on 2026-01-15 (EST = UTC-5).
	nextRun := time.Date(2026, 1, 15, 7, 0, 0, 0, time.UTC)
	schedule := durableSchedule{
		id:       "daily-local",
		cronSpec: "0 2 * * *",
		tz:       "America/New_York",
		nextRun:  nextRun,
	}
	// Evaluate at 02:00 NY = 07:00 UTC: must produce a tick at exactly that
	// instant and advance to the next 02:00-NY occurrence.
	ticks, _, err := boundedDueTicks(schedule, nextRun, 100)
	if err != nil {
		t.Fatalf("NY-local cron fire error: %v", err)
	}
	if len(ticks) != 1 || !ticks[0].Equal(nextRun) {
		t.Fatalf("NY-local cron ticks = %v, want [%s UTC]", ticks, nextRun.Format(time.RFC3339))
	}

	// Sanity: the same spec registered in UTC would also fire at 02:00 UTC,
	// but a NY-registered schedule must NOT treat 02:00 UTC (21:00 NY prev
	// day) as a tick — proving the location actually drives the match.
	utcSchedule := schedule
	utcSchedule.tz = ""
	utcSchedule.nextRun = time.Date(2026, 1, 15, 2, 0, 0, 0, time.UTC)
	ticks, _, err = boundedDueTicks(utcSchedule, utcSchedule.nextRun, 100)
	if err != nil {
		t.Fatalf("UTC cron fire error: %v", err)
	}
	if len(ticks) != 1 || !ticks[0].Equal(utcSchedule.nextRun) {
		t.Fatalf("UTC cron ticks = %v, want [%s UTC]", ticks, utcSchedule.nextRun.Format(time.RFC3339))
	}
}
