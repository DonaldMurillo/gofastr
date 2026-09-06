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
// already current — or, after a crash between the file write and the snapshot
// save, that the last committed migration already carries this delta: the
// re-run recognizes it by content, repairs the snapshot, and writes nothing.
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

	// Crash reconcile: writing the migration file and saving the snapshot are
	// two steps, and a crash (or a failed SaveSnapshot — the same on-disk
	// state) between them leaves the delta committed as migration N next to a
	// stale snapshot. A re-run regenerates the SAME delta from the stale
	// snapshot and would mint it again as N+1 — a duplicate whose Up can
	// never apply cleanly (its CREATE TABLE hits the table N just made) and
	// marks the database dirty mid-deploy. Detect the state by content: when
	// the LAST committed migration's Up and Down sections match this delta,
	if lastUp, lastDown, ok := lastCommittedSections(opts.MigrationsDir); ok &&
		strings.TrimSpace(lastUp) == strings.TrimSpace(up) &&
		strings.TrimSpace(lastDown) == strings.TrimSpace(down) {
		if err := SaveSnapshot(snapPath, next); err != nil {
			return "", fmt.Errorf("snapshot repair failed (migration already committed): %w", err)
		}
		return "", nil
	}

	version := nextMigrationVersion(opts.MigrationsDir)
	slug := sanitizeMigrationName(name)
	filename := fmt.Sprintf("%04d_%s.sql", version, slug)
	path := filepath.Join(opts.MigrationsDir, filename)
	content, err := RenderMigrationFileChecked(version, slug, up, down)
	if err != nil {
		return "", err
	}
	if afterVersionScan != nil {
		afterVersionScan(opts.MigrationsDir, filename)
	}
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
	// Kernel-contained create: the file is written through an *os.Root
	// over MigrationsDir with O_EXCL, so a symlink planted at the chosen
	// filename after the version scan is refused by the kernel instead
	// of followed outside the directory — and a pre-existing entry at
	// the name is refused rather than clobbered. The numbering makes a
	// collision impossible for honest writers (the scan counts every
	// NNNN_*.sql entry), so O_EXCL only ever fires on a plant or a race.
	root, oerr := os.OpenRoot(opts.MigrationsDir)
	if oerr != nil {
		return "", fmt.Errorf("open %s: %w", opts.MigrationsDir, oerr)
	}
	defer root.Close()
	w, werr := root.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if werr != nil {
		return "", fmt.Errorf("write %s: %w", path, werr)
	}
	if _, werr = w.Write([]byte(content)); werr != nil {
		w.Close()
		return "", fmt.Errorf("write %s: %w", path, werr)
	}
	if werr = w.Close(); werr != nil {
		return "", fmt.Errorf("write %s: %w", path, werr)
	}
	if err := SaveSnapshot(snapPath, next); err != nil {
		return "", fmt.Errorf("snapshot update failed (migration written): %w", err)
	}
	return path, nil
}

// afterVersionScan is a test seam: when non-nil it runs after the next
// migration version has been scanned and the filename chosen, and
// immediately before the file is created. The scan has already committed
// to the name, so a symlink planted here is invisible to the numbering
// and is followed by a plain os.WriteFile. Nothing installs it outside
// the security tests (same pattern as core/router's serveHook); tests
// using it must not run in parallel.
var afterVersionScan func(dir, filename string)

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

// lastCommittedSections parses the highest-versioned NNNN_*.sql file in dir —
// the last committed migration — and returns its Up and Down section bodies.
// ok is false when the directory holds no migration file or the file has no
// Up directive; reconcile then simply does not fire and generation proceeds.
func lastCommittedSections(dir string) (up, down string, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", false
	}
	var best uint64
	var bestName string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix := e.Name()
		if i := strings.IndexByte(prefix, '_'); i > 0 {
			prefix = prefix[:i]
		}
		v, err := strconv.ParseUint(prefix, 10, 64)
		if err != nil || v < best || (v == best && e.Name() <= bestName && bestName != "") {
			continue
		}
		best, bestName = v, e.Name()
	}
	if bestName == "" {
		return "", "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, bestName))
	if err != nil {
		return "", "", false
	}
	return parseMigrationSections(string(data))
}

// parseMigrationSections extracts the Up and Down bodies from a rendered
// `-- +migrate` file: everything after the "-- +migrate Up" line up to the
// next "-- +migrate" directive or EOF. A Group directive stamped above Up is
// skipped; the Version/Name header directives never open a section. ok is
// false when the content has no Up directive at all.
func parseMigrationSections(content string) (up, down string, ok bool) {
	if !strings.Contains(content, "-- +migrate Up") {
		return "", "", false
	}
	var cur *string
	for _, ln := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "-- +migrate") {
			switch trimmed {
			case "-- +migrate Up":
				cur = &up
			case "-- +migrate Down":
				cur = &down
			default:
				cur = nil
			}
			continue
		}
		if cur != nil {
			*cur += ln
			*cur += "\n"
		}
	}
	return up, down, true
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
