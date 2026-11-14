package main

import (
	"path/filepath"
	"testing"
)

// The telemetry files follow the database: a test that starts Meridian
// against a database in its own temporary directory must not leave day
// files in the source tree, where every repo-walking test would see the
// tree change under it.
func TestTelemetryDirFollowsTheDatabase(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ name, database, set, want string }{
		{"beside a database elsewhere", "file:" + filepath.Join(dir, "e2e.db"), "", filepath.Join(dir, "meridian-telemetry")},
		{"options on the file are not part of its path", "file:" + filepath.Join(dir, "e2e.db") + "?_busy_timeout=5000", "", filepath.Join(dir, "meridian-telemetry")},
		{"the default database", "", "", "meridian-telemetry"},
		{"not a file", "postgres://localhost/meridian", "", "meridian-telemetry"},
		{"said outright", "file:" + filepath.Join(dir, "e2e.db"), filepath.Join(dir, "elsewhere"), filepath.Join(dir, "elsewhere")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", c.database)
			t.Setenv("TELEMETRY_DIR", c.set)
			if got := telemetryDir(); got != c.want {
				t.Errorf("telemetryDir() = %q, want %q", got, c.want)
			}
		})
	}
}
