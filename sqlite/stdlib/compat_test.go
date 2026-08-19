package stdlib

import (
	"database/sql"
	"testing"
	"time"
)

// canonicalLayout is the on-disk timestamp layout GoFastr reads back. It is
// what mattn/go-sqlite3 wrote, so existing databases stay readable, and it is
// the first layout battery/auth's parseTimeFlex tries.
const canonicalLayout = "2006-01-02 15:04:05.999999999-07:00"

// TestTimeBindsInCanonicalLayout is the regression guard for a silent auth
// outage: modernc's default bind format is Go's String()
// ("... +0000 UTC"), which NOTHING in the repo parses. A session written
// with it read back as the zero time, so every session looked expired and
// login/register never stuck.
func TestTimeBindsInCanonicalLayout(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ref := time.Date(2026, 7, 20, 23, 59, 59, 123456789, time.UTC)
	var stored string
	if err := db.QueryRow("SELECT CAST(? AS TEXT)", ref).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	got, err := time.Parse(canonicalLayout, stored)
	if err != nil {
		t.Fatalf("bound time %q does not parse as the canonical layout %q: %v",
			stored, canonicalLayout, err)
	}
	if !got.Equal(ref) {
		t.Fatalf("round-tripped time = %v, want %v", got, ref)
	}
}

// TestTimeRoundTripsThroughTextColumn exercises the exact shape that broke:
// a timestamp written into a TEXT column and read back as a string.
func TestTimeRoundTripsThroughTextColumn(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE sessions (token TEXT, expires_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	future := time.Now().UTC().Add(time.Hour)
	if _, err := db.Exec(`INSERT INTO sessions VALUES (?, ?)`, "tok", future); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT expires_at FROM sessions WHERE token = ?`, "tok").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	got, err := time.Parse(canonicalLayout, raw)
	if err != nil {
		t.Fatalf("stored expires_at %q is unparseable: %v", raw, err)
	}
	if !got.After(time.Now().UTC()) {
		t.Fatalf("expires_at %v parsed to a past time — session would look expired", got)
	}
}

func TestWithCompatDefaultsRespectsExplicitSettings(t *testing.T) {
	const fk = "&_pragma=foreign_keys(1)"
	cases := []struct{ in, want string }{
		{":memory:", ":memory:?_time_format=sqlite&_pragma=busy_timeout(5000)" + fk},
		{"./blog.db", "./blog.db?_time_format=sqlite&_pragma=busy_timeout(5000)" + fk},
		{":memory:?cache=shared", ":memory:?cache=shared&_time_format=sqlite&_pragma=busy_timeout(5000)" + fk},
		{"file:/tmp/x.db?_journal=WAL", "file:/tmp/x.db?_journal=WAL&_time_format=sqlite&_pragma=busy_timeout(5000)" + fk},
		// Explicit caller settings win.
		{"x.db?_time_format=datetime", "x.db?_time_format=datetime&_pragma=busy_timeout(5000)" + fk},
		{"x.db?_time_integer_format=unix", "x.db?_time_integer_format=unix&_pragma=busy_timeout(5000)" + fk},
		{"x.db?_pragma=busy_timeout(1)", "x.db?_pragma=busy_timeout(1)&_time_format=sqlite" + fk},
		// An explicit foreign_keys choice is left alone in BOTH directions —
		// the opt-out is the upgrade path for an app holding dangling rows,
		// and re-adding the default beside it would silently override it.
		{"x.db?_pragma=foreign_keys(0)", "x.db?_pragma=foreign_keys(0)&_time_format=sqlite&_pragma=busy_timeout(5000)"},
		{"x.db?_pragma=foreign_keys(1)", "x.db?_pragma=foreign_keys(1)&_time_format=sqlite&_pragma=busy_timeout(5000)"},
		// A query url.ParseQuery cannot decode. The driver will reject such a
		// DSN outright, so this is unobservable through a live connection —
		// but the fallback still must not append a default beside the opt-out
		// sitting right next to the malformed pair, or a later driver that
		// tolerated it would silently invert the caller's choice.
		{"x.db?_pragma=foreign_keys%280%29&%zz=1", "x.db?_pragma=foreign_keys%280%29&%zz=1&_time_format=sqlite&_pragma=busy_timeout(5000)"},
	}
	for _, c := range cases {
		if got := withCompatDefaults(c.in); got != c.want {
			t.Errorf("withCompatDefaults(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBusyTimeoutDefaultsToMattnValue guards a concurrency regression from
// the driver swap. mattn/go-sqlite3 defaulted busy_timeout to 5000ms; modernc
// defaults to 0, so a second writer fails instantly with SQLITE_BUSY instead
// of waiting. Apps written against mattn assume the wait.
func TestBusyTimeoutDefaultsToMattnValue(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != 5000 {
		t.Fatalf("busy_timeout = %d, want 5000 (mattn's default; 0 fails instantly under contention)", got)
	}
}

func TestBusyTimeoutRespectsExplicitSetting(t *testing.T) {
	for _, dsn := range []string{
		":memory:?_pragma=busy_timeout(250)",
		":memory:?_busy_timeout=250",
	} {
		db, err := sql.Open("sqlite3", dsn)
		if err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		var got int
		if err := db.QueryRow("PRAGMA busy_timeout").Scan(&got); err != nil {
			t.Fatalf("%s: %v", dsn, err)
		}
		db.Close()
		if got != 250 {
			t.Errorf("%s: busy_timeout = %d, want the caller's 250", dsn, got)
		}
	}
}

// SQLite ignores FOREIGN KEY clauses unless the pragma is on, so before this
// default an app whose migrator wrote the constraints still accepted a
// relation column pointing at nothing. AutoMigrate emits exactly the DDL
// below, so this is the shape that was silently unenforced.
func TestForeignKeysAreEnforcedByDefault(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("read pragma: %v", err)
	}
	if on != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", on)
	}

	mustExecDDL(t, db, `CREATE TABLE orders (id TEXT PRIMARY KEY)`)
	mustExecDDL(t, db, `CREATE TABLE items (id TEXT PRIMARY KEY, order_id TEXT, FOREIGN KEY (order_id) REFERENCES orders(id))`)
	mustExecDDL(t, db, `INSERT INTO orders (id) VALUES ('o1')`)

	// A real parent still resolves — the default must not break valid writes.
	mustExecDDL(t, db, `INSERT INTO items (id, order_id) VALUES ('i1','o1')`)

	// A NULL relation column is not a dangling reference.
	mustExecDDL(t, db, `INSERT INTO items (id, order_id) VALUES ('i2', NULL)`)

	if _, err := db.Exec(`INSERT INTO items (id, order_id) VALUES ('i3','no-such-order')`); err == nil {
		t.Error("an insert naming a nonexistent parent was accepted — foreign keys are not enforced")
	}
}

// The default is a behavior change for apps already holding dangling rows,
// so the escape hatch has to actually work.
func TestForeignKeysDSNOptOutWins(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:?_pragma=foreign_keys(0)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var on int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("read pragma: %v", err)
	}
	if on != 0 {
		t.Fatalf("PRAGMA foreign_keys = %d with an explicit opt-out, want 0", on)
	}
}

func mustExecDDL(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// The guard that decides "the caller already chose" is a substring test, so it
// has to match the spellings modernc actually honors and no others. A DSN
// naming `foreign_keys` in a form modernc ignores would suppress the default
// AND do nothing itself — the one outcome worse than either, because the DSN
// reads as opted-in while enforcement is off.
func TestForeignKeysUnknownSpellingStillGetsTheDefault(t *testing.T) {
	cases := []struct {
		dsn  string
		want int
		why  string
	}{
		{":memory:", 1, "no choice expressed — the default applies"},
		{":memory:?_pragma=foreign_keys(0)", 0, "the documented opt-out"},
		{":memory:?_pragma=foreign_keys(1)", 1, "explicitly on"},
		{":memory:?_foreign_keys=0", 0, "mattn spelling, honored by modernc"},
		// modernc ignores a bare `foreign_keys=` — it is neither a _pragma nor
		// the mattn parameter. Skipping the default for it left FKs off.
		{":memory:?foreign_keys=1", 1, "unknown spelling must not suppress the default"},
		{":memory:?_pragma=foreign_keys", 1, "valueless _pragma sets nothing"},
		// An empty shorthand is not a choice. modernc suppresses the
		// shorthand's own pragma when the value is empty, so reading the bare
		// KEY as an opt-out turned enforcement off for a DSN that merely
		// mentioned the parameter. Executed against the shipped driver before
		// the fix: both of these reported 0.
		{":memory:?_foreign_keys=", 1, "empty mattn shorthand selects nothing"},
		{":memory:?_fk=", 1, "empty _fk shorthand selects nothing"},
		{":memory:?_fk=0", 0, "a non-empty _fk IS a choice"},
	}
	for _, c := range cases {
		db, err := sql.Open("sqlite3", c.dsn)
		if err != nil {
			t.Fatalf("open %q: %v", c.dsn, err)
		}
		var on int
		if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&on); err != nil {
			db.Close()
			t.Fatalf("read pragma for %q: %v", c.dsn, err)
		}
		db.Close()
		if on != c.want {
			t.Errorf("%q → PRAGMA foreign_keys = %d, want %d (%s)", c.dsn, on, c.want, c.why)
		}
	}
}

// The driver URL-decodes query values and applies `_pragma` settings in sorted
// order, so `foreign_keys(1)` sorts after `foreign_keys(0)` and executes last.
// A substring guard that misses an encoded or differently-cased spelling
// therefore does worse than ignore it: the appended default OVERRIDES the
// caller's explicit opt-out. That is the same "the DSN says one thing and the
// connection does another" failure the guard was written to fix, inverted.
func TestForeignKeysOptOutSurvivesEncodingAndCase(t *testing.T) {
	cases := []struct {
		dsn  string
		want int
	}{
		{":memory:?_pragma=foreign_keys(0)", 0},
		{":memory:?_pragma=foreign_keys%280%29", 0},
		{":memory:?_pragma=FOREIGN_KEYS(0)", 0},
		{":memory:?_pragma=FOREIGN_KEYS%280%29", 0},
		{":memory:?_pragma=Foreign_Keys(0)", 0},
		{":memory:?_pragma=busy_timeout(1)&_pragma=foreign_keys(0)", 0},
		// SQLite's PRAGMA grammar is `name(v)`, `name = v`, and `name=v` with
		// arbitrary whitespace, and the driver runs all of them. Keying on one
		// spelling let the others through: the driver honoured the opt-out
		// while the guard said "no choice made", so the appended default was
		// applied and won on sort order.
		{":memory:?_pragma=foreign_keys (0)", 0},
		{":memory:?_pragma=foreign_keys (0 )", 0},
		{":memory:?_pragma=FOREIGN_KEYS (0)", 0},
		{":memory:?_pragma=foreign_keys%20(0)", 0},
		{":memory:?_pragma=foreign_keys = 0", 0},
		{":memory:?_pragma=foreign_keys=0", 0},
		{":memory:?_foreign_keys=0", 0},
		{":memory:?_fk=0", 0},
		// On is the default and stays on however it is spelled.
		{":memory:", 1},
		{":memory:?_pragma=foreign_keys(1)", 1},
		{":memory:?_pragma=FOREIGN_KEYS%281%29", 1},
		// A spelling the driver ignores must not suppress the default.
		{":memory:?foreign_keys=0", 1},
		{":memory:?_pragma=foreign_keys", 1},
	}
	for _, c := range cases {
		db, err := sql.Open("sqlite3", c.dsn)
		if err != nil {
			t.Fatalf("open %q: %v", c.dsn, err)
		}
		var on int
		err = db.QueryRow(`PRAGMA foreign_keys`).Scan(&on)
		db.Close()
		if err != nil {
			t.Fatalf("read pragma for %q: %v", c.dsn, err)
		}
		if on != c.want {
			t.Errorf("%q → PRAGMA foreign_keys = %d, want %d", c.dsn, on, c.want)
		}
	}
}
