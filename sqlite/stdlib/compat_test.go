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
	cases := []struct{ in, want string }{
		{":memory:", ":memory:?_time_format=sqlite&_pragma=busy_timeout(5000)"},
		{"./blog.db", "./blog.db?_time_format=sqlite&_pragma=busy_timeout(5000)"},
		{":memory:?cache=shared", ":memory:?cache=shared&_time_format=sqlite&_pragma=busy_timeout(5000)"},
		{"file:/tmp/x.db?_journal=WAL", "file:/tmp/x.db?_journal=WAL&_time_format=sqlite&_pragma=busy_timeout(5000)"},
		// Explicit caller settings win.
		{"x.db?_time_format=datetime", "x.db?_time_format=datetime&_pragma=busy_timeout(5000)"},
		{"x.db?_time_integer_format=unix", "x.db?_time_integer_format=unix&_pragma=busy_timeout(5000)"},
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
