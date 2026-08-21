package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
)

// MigrationFileOptions configures GenerateMigrationFile.
type MigrationFileOptions struct {
	// MigrationsDir is the directory the versioned .sql files are written to.
	MigrationsDir string
	// SnapshotPath is the committed schema snapshot the generation diffs
	// against and updates. Defaults to <MigrationsDir>/schema.snapshot.json
	// when empty.
	SnapshotPath string
	// Dialect controls the emitted SQL types (SQLite vs Postgres).
	Dialect Dialect
	// Group stamps a "-- +migrate Group <name>" directive into the generated
	// file, scoping it into a named migration group. Empty = default group.
	Group string
}

// GenerateMigrationFile diffs the Plan against the committed snapshot at
// opts.SnapshotPath, writes the next numbered migration file into
// opts.MigrationsDir, and updates the snapshot: the offline declarative
// workflow driven from a Plan (entity registry + raw Tables + Views +
// Routines) rather than a blueprint. It is the supported entrypoint a host
// binary calls from its own main() to emit versioned migrations from its
// compiled entity registry:
//
//	plan := migrate.Plan{Registry: app.Registry}
//	path, err := migrate.GenerateMigrationFile(plan, "add_email", migrate.MigrationFileOptions{
//	    MigrationsDir: "migrations",
//	    SnapshotPath:  "migrations/schema.snapshot.json",
//	    Dialect:       migrate.DialectSQLite,
//	})
//
// The output matches `gofastr migrate generate --from=<blueprint>` exactly:
// same NNNN_name.sql naming, same -- +migrate directive layout, same snapshot
// format, because both paths use the same GeneratePlan + RenderMigrationFile +
// SaveSnapshot primitives.
//
// Returns the written file path. An empty path (nil error) means the schema is
// already current. Nothing was written.
func GenerateMigrationFile(plan Plan, name string, opts MigrationFileOptions) (string, error) {
	// Validate the group before writing anything: an invalid name would be
	// stamped into a directive the runner then refuses, leaving a committed
	// migration nothing can apply. The CLI validates too; a host binary
	// calling this directly gets the same guarantee.
	if opts.Group != "" {
		if err := coremig.ValidateGroupName(opts.Group); err != nil {
			return "", err
		}
	}

	snapPath := opts.SnapshotPath
	if snapPath == "" {
		snapPath = filepath.Join(opts.MigrationsDir, "schema.snapshot.json")
	}

	prev, err := LoadSnapshot(snapPath)
	if err != nil {
		return "", fmt.Errorf("read snapshot %s: %w", snapPath, err)
	}

	up, down, next, err := GeneratePlan(plan, prev, opts.Dialect)
	if err != nil {
		return "", fmt.Errorf("generate: %w", err)
	}
	if up == "" {
		return "", nil // schema is up to date
	}

	if err := os.MkdirAll(opts.MigrationsDir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", opts.MigrationsDir, err)
	}
	version := nextMigrationVersion(opts.MigrationsDir)
	slug := sanitizeMigrationName(name)
	filename := fmt.Sprintf("%04d_%s.sql", version, slug)
	path := filepath.Join(opts.MigrationsDir, filename)

	content := RenderMigrationFile(version, slug, up, down)
	if opts.Group != "" {
		// Stamp the -- +migrate Group directive just before the Up section so
		// the runner scopes this migration into the named group. Fail loudly
		// rather than silently dropping the group if the anchor ever moves.
		stamped := strings.Replace(content, "-- +migrate Up\n",
			"-- +migrate Group "+opts.Group+"\n-- +migrate Up\n", 1)
		if stamped == content {
			return "", fmt.Errorf("could not stamp group %q: no -- +migrate Up directive in the rendered migration", opts.Group)
		}
		content = stamped
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if err := SaveSnapshot(snapPath, next); err != nil {
		return "", fmt.Errorf("snapshot update failed (migration written): %w", err)
	}
	return path, nil
}

// nextMigrationVersion returns one past the highest NNNN_ prefix among the
// existing .sql files, or 1 when the directory is empty. Matches the blueprint
// CLI's version-numbering exactly (cmd/gofastr/migrate_generate.go).
func nextMigrationVersion(dir string) uint64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}
	var max uint64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix := e.Name()
		if i := strings.IndexByte(prefix, '_'); i > 0 {
			prefix = prefix[:i]
		}
		if v, err := strconv.ParseUint(prefix, 10, 64); err == nil && v > max {
			max = v
		}
	}
	return max + 1
}

// sanitizeMigrationName lower-cases and replaces non-alphanumeric runs with a
// single underscore so the name is filesystem- and directive-safe. Matches the
// blueprint CLI's sanitization exactly (cmd/gofastr/migrate_generate.go).
func sanitizeMigrationName(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "migration"
	}
	return out
}
