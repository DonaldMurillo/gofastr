//go:build linux && (amd64 || arm64)

package linux

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const profileTestID = "dev.gofastr.profile-test"

func TestProfileOriginAndLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "appdata")
	p, err := openBrowserProfile(profileTestID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.addr != "127.0.0.1:0" {
		t.Fatal(p.addr)
	}
	if _, err := openBrowserProfile(profileTestID, dir); err == nil {
		t.Fatal("second profile owner accepted")
	}
	if err := p.commitOrigin("127.0.0.1:43127"); err != nil {
		t.Fatal(err)
	}
	if err := p.commitOrigin("127.0.0.1:43128"); err == nil {
		t.Fatal("remapped origin accepted")
	}
	if err := p.lock.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := openBrowserProfile(profileTestID, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.lock.Close()
	if second.addr != "127.0.0.1:43127" {
		t.Fatal(second.addr)
	}
	isolated, err := openBrowserProfile("dev.gofastr.other", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.lock.Close()
	if isolated.dir == second.dir || isolated.addr != "127.0.0.1:0" {
		t.Fatal("AppID profiles overlap")
	}
}
func TestProfileFailedStartupRetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "appdata")
	s := New().(*linuxShell)
	if _, err := s.PrepareProfile(profileTestID, dir); err != nil {
		t.Fatal(err)
	}
	s.ReleaseProfile()
	s.ReleaseProfile()
	next := New().(*linuxShell)
	if addr, err := next.PrepareProfile(profileTestID, dir); err != nil || addr != "127.0.0.1:0" {
		t.Fatalf("retry: addr=%s err=%v", addr, err)
	}
	next.ReleaseProfile()
}
func TestProfileUnsafePaths(t *testing.T) {
	for _, kind := range []string{"data-symlink", "profile-symlink", "cookie-symlink", "cookie-fifo", "cookie-hardlink", "cookie-public", "cookie-corrupt", "sidecar-public", "nested-symlink", "data-public", "missing-origin", "pending-with-data"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "appdata")
			p, err := openBrowserProfile(profileTestID, dir)
			if err != nil {
				t.Fatal(err)
			}
			if kind != "pending-with-data" {
				if err := p.commitOrigin("127.0.0.1:43127"); err != nil {
					t.Fatal(err)
				}
			}
			p.lock.Close()
			cookie := filepath.Join(p.dir, "cookies.sqlite")
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "data-symlink":
				real := dir + "-real"
				must(os.Rename(dir, real))
				t.Cleanup(func() { os.RemoveAll(real) })
				must(os.Symlink(real, dir))
			case "profile-symlink":
				real := p.dir + "-real"
				must(os.Rename(p.dir, real))
				must(os.Symlink(real, p.dir))
			case "cookie-symlink":
				must(os.Remove(cookie))
				must(os.Symlink(filepath.Join(t.TempDir(), "outside"), cookie))
			case "cookie-fifo":
				must(os.Remove(cookie))
				must(syscall.Mkfifo(cookie, 0o600))
			case "cookie-hardlink":
				must(os.Link(cookie, filepath.Join(dir, "alias")))
			case "cookie-public":
				must(os.Chmod(cookie, 0o644))
			case "cookie-corrupt":
				must(os.WriteFile(cookie, []byte("not a SQLite database"), 0o600))
			case "sidecar-public":
				must(os.WriteFile(cookie+"-wal", []byte("x"), 0o600))
				must(os.Chmod(cookie+"-wal", 0o644)) // Do not let a restrictive umask mask this fixture.
			case "nested-symlink":
				must(os.Symlink(t.TempDir(), filepath.Join(p.dir, "data", "alias")))
			case "data-public":
				must(os.Chmod(dir, 0o755))
			case "missing-origin":
				must(os.Remove(filepath.Join(p.dir, "origin.json")))
			case "pending-with-data":
				must(os.WriteFile(filepath.Join(p.dir, "data", "stored"), []byte("native"), 0o600))
			}
			if accepted, err := openBrowserProfile(profileTestID, dir); err == nil {
				accepted.lock.Close()
				t.Fatal("unsafe profile accepted")
			}
		})
	}
}

func TestProfileCookieFilenameFamilyIsPrivate(t *testing.T) {
	// This is intentionally a filename-prefix policy, not a path-segment
	// selector. Unknown siblings and backup suffixes must not escape it.
	for _, name := range []string{"cookies.sqlite", "cookies.sqlite-wal", "cookies.sqlite-shm", "cookies.sqlite-journal", "cookies.sqlite.backup", "cookies.sqlitex", "origin.json", "profile.lock"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("private fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := inspectProfileTree(dir); err != nil {
				t.Fatal("owner-only record refused: ", err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := inspectProfileTree(dir); err == nil || !strings.Contains(err.Error(), "not private") {
				t.Fatalf("readable cookie/metadata sibling accepted: %v", err)
			}
		})
	}
}

// WebKit's network cache stores a body of 16 KiB or more once under
// Blobs/ and hard-links it into each record, so a relaunch must accept a
// link that stays inside the profile. A link to a file outside it, or a
// second name for cookie or origin data, is still refused.
func TestProfileHardLinks(t *testing.T) {
	blobs := filepath.Join("cache", "WebKitCache", "Version 17", "Blobs")
	records := filepath.Join("cache", "WebKitCache", "Version 17", "Records", "P", "Resource")
	setup := func(t *testing.T) string {
		dir := t.TempDir()
		for _, d := range []string{blobs, records} {
			if err := os.MkdirAll(filepath.Join(dir, d), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, blobs, "5D4A"), []byte("cached body"), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	t.Run("in-tree blob link", func(t *testing.T) {
		dir := setup(t)
		if err := os.Link(filepath.Join(dir, blobs, "5D4A"), filepath.Join(dir, records, "R1-blob")); err != nil {
			t.Fatal(err)
		}
		if err := inspectProfileTree(dir); err != nil {
			t.Fatal("WebKit blob link refused: ", err)
		}
	})
	t.Run("link leaves the profile", func(t *testing.T) {
		dir := setup(t)
		if err := os.Link(filepath.Join(dir, blobs, "5D4A"), filepath.Join(t.TempDir(), "outside")); err != nil {
			t.Fatal(err)
		}
		if err := inspectProfileTree(dir); err == nil || !strings.Contains(err.Error(), "outside the browser profile") {
			t.Fatalf("link to a file outside the profile accepted: %v", err)
		}
	})
	t.Run("cookie store link", func(t *testing.T) {
		dir := setup(t)
		path := filepath.Join(dir, "cookies.sqlite")
		if err := os.WriteFile(path, []byte("private fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, filepath.Join(dir, "data-copy")); err != nil {
			t.Fatal(err)
		}
		if err := inspectProfileTree(dir); err == nil || !strings.Contains(err.Error(), "not private") {
			t.Fatalf("hard-linked cookie store accepted: %v", err)
		}
	})
}

func TestProfileOriginValidation(t *testing.T) {
	for _, body := range []string{
		`{"version":1,"app_id":"dev.gofastr.profile-test","port":43127,"port":43128}`,
		`{"version":1,"app_id":"dev.gofastr.profile-test","Port":43127}`,
		`{"version":1,"app_id":"dev.gofastr.other","port":43127}`,
		`{"version":1,"app_id":"dev.gofastr.profile-test","port":80}`,
		`{"version":2,"app_id":"dev.gofastr.profile-test","port":43127}`,
		`{"version":1,"app_id":"dev.gofastr.profile-test"}`,
		`{"version":1,"app_id":"dev.gofastr.profile-test","port":null}`,
		`null`, `{}`, `{} {}`,
		`{"version":1,"app_id":"dev.gofastr.profile-test","port":43127}` + strings.Repeat(" ", 1100) + "x",
	} {
		t.Run(fmt.Sprintf("case-%d", len(body)), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "appdata")
			p, err := openBrowserProfile(profileTestID, dir)
			if err != nil {
				t.Fatal(err)
			}
			p.lock.Close()
			if err := os.WriteFile(filepath.Join(p.dir, "origin.json"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if accepted, err := openBrowserProfile(profileTestID, dir); err == nil {
				accepted.lock.Close()
				t.Fatal("invalid origin accepted")
			}
		})
	}
}
func TestProfileValidCookieDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "appdata")
	p, err := openBrowserProfile(profileTestID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.commitOrigin("127.0.0.1:43127"); err != nil {
		t.Fatal(err)
	}
	p.lock.Close()
	db, err := sql.Open("sqlite3", filepath.Join(p.dir, "cookies.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(nativeCookieSchema); err != nil {
		t.Fatal(err)
	}
	db.Close()
	next, err := openBrowserProfile(profileTestID, dir)
	if err != nil {
		t.Fatal(err)
	}
	next.lock.Close()
}

const nativeCookieSchema = "CREATE TABLE moz_cookies (id INTEGER PRIMARY KEY, name TEXT, value TEXT, host TEXT, path TEXT, expiry INTEGER, lastAccessed INTEGER, isSecure INTEGER, isHttpOnly INTEGER, sameSite INTEGER)"

func TestProfileCookieSchema(t *testing.T) {
	for _, test := range []struct {
		name, schema string
		valid        bool
	}{
		{"current", nativeCookieSchema, true},
		{"legacy", strings.Replace(nativeCookieSchema, ", sameSite INTEGER", "", 1), true},
		{"wrong-table", "CREATE TABLE cookies(name TEXT, value TEXT)", false},
		{"view", "CREATE VIEW moz_cookies AS SELECT 1 AS id", false},
		{"extra-column", strings.TrimSuffix(nativeCookieSchema, ")") + ", extra TEXT)", false},
		{"missing-column", strings.Replace(nativeCookieSchema, ", expiry INTEGER", "", 1), false},
		{"wrong-type", strings.Replace(nativeCookieSchema, "expiry INTEGER", "expiry TEXT", 1), false},
		{"not-null", strings.Replace(nativeCookieSchema, "lastAccessed INTEGER", "lastAccessed INTEGER NOT NULL", 1), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cookies.sqlite")
			db, err := sql.Open("sqlite3", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(test.schema); err != nil {
				t.Fatal(err)
			}
			db.Close()
			err = checkCookieDatabase(path)
			if (err == nil) != test.valid {
				t.Fatalf("schema validation=%v valid=%v", err, test.valid)
			}
		})
	}
}

// A crash mid-write leaves a hot rollback journal. A read-only check cannot
// roll it back (SQLITE_READONLY_ROLLBACK) and would block every later launch.
func TestCookieCheckRollsBackHotJournal(t *testing.T) {
	src := filepath.Join(t.TempDir(), "cookies.sqlite")
	db, err := sql.Open("sqlite3", src)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=DELETE", nativeCookieSchema, "INSERT INTO moz_cookies(name) VALUES ('kept')", "PRAGMA cache_size=1"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	// A tiny cache spills the uncommitted pages to the file, so a copy taken
	// now is the state a crash leaves behind: a half-written file plus a hot
	// journal holding the original pages.
	if _, err := tx.Exec("WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<5000) INSERT INTO moz_cookies(name, value) SELECT 'torn', hex(randomblob(200)) FROM n"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cookies.sqlite")
	for _, suffix := range []string{"", "-journal"} {
		data, err := os.ReadFile(src + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+suffix, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_ = tx.Rollback()
	if err := checkCookieDatabase(path); err != nil {
		t.Fatal("hot journal blocked the cookie check: ", err)
	}
	if _, err := os.Stat(path + "-journal"); !os.IsNotExist(err) {
		t.Fatal("hot journal was not rolled back: ", err)
	}
	check, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	var torn int
	if err := check.QueryRow("SELECT count(*) FROM moz_cookies WHERE name = 'torn'").Scan(&torn); err != nil || torn != 0 {
		t.Fatalf("rollback left torn rows: %d %v", torn, err)
	}
}

// An abrupt process exit releases the kernel lock without deleting its file.
// There is no manual unlock or stale-PID guess.
func TestProfileCrashReleasesLock(t *testing.T) {
	if dir := os.Getenv("GOFASTR_PROFILE_LOCK_CHILD"); dir != "" {
		p, err := openBrowserProfile(profileTestID, dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := p.commitOrigin("127.0.0.1:43127"); err != nil {
			t.Fatal(err)
		}
		os.Exit(0)
	}
	dir := filepath.Join(t.TempDir(), "appdata")
	cmd := exec.Command(os.Args[0], "-test.run=^TestProfileCrashReleasesLock$")
	cmd.Env = append(os.Environ(), "GOFASTR_PROFILE_LOCK_CHILD="+dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v %s", err, output)
	}
	p, err := openBrowserProfile(profileTestID, dir)
	if err != nil {
		t.Fatal(err)
	}
	p.lock.Close()
	if p.addr != "127.0.0.1:43127" {
		t.Fatal("origin lost after process exit")
	}
}
func TestProfileIdentityValidation(t *testing.T) {
	for _, id := range []string{"", "title", "..", "../escape", "a.b/../c", "a.b\x00", "a.b_"} {
		if p, err := openBrowserProfile(id, t.TempDir()); err == nil {
			p.lock.Close()
			t.Fatalf("accepted ID %q", id)
		}
	}
	for _, path := range []string{"relative", "/", "\x00", filepath.Join(t.TempDir(), "invalid-\xff")} {
		if p, err := openBrowserProfile(profileTestID, path); err == nil {
			p.lock.Close()
			t.Fatalf("accepted path %q", path)
		}
	}
}
