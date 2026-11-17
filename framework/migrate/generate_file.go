package migrate

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
)

// MigrationFileOptions configures GenerateMigrationFile.
type MigrationFileOptions struct {
	// MigrationsDir is the directory the versioned .sql files are written to.
	MigrationsDir string
	// SnapshotPath is the snapshot diffed against and updated; empty means
	// <MigrationsDir>/schema.snapshot.json.
	SnapshotPath string
	// Dialect controls the emitted SQL types (SQLite vs Postgres).
	Dialect Dialect
	// Group stamps a "-- +migrate Group <name>" directive into the file;
	// empty is the default group.
	Group string
}

// GenerateMigrationFile diffs plan against the snapshot at
// opts.SnapshotPath, writes the next NNNN_name.sql migration into
// opts.MigrationsDir and updates the snapshot. It returns the written path;
// an empty path with a nil error means the schema is current, or that the
// last committed migration already holds this delta and only the snapshot
// was repaired. An invalid opts.Group is refused before anything is written.
func GenerateMigrationFile(plan Plan, name string, opts MigrationFileOptions) (string, error) {
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

	if lastUp, lastDown, ok := lastCommittedSections(opts.MigrationsDir); ok &&
		strings.TrimSpace(lastUp) == strings.TrimSpace(up) &&
		strings.TrimSpace(lastDown) == strings.TrimSpace(down) {
		if err := atomicfile.SyncDir(opts.MigrationsDir); err != nil {
			return "", fmt.Errorf("sync migrations before snapshot repair: %w", err)
		}
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
		stamped := strings.Replace(content, "-- +migrate Up\n",
			"-- +migrate Group "+opts.Group+"\n-- +migrate Up\n", 1)
		if stamped == content {
			return "", fmt.Errorf("could not stamp group %q: no -- +migrate Up directive in the rendered migration", opts.Group)
		}
		content = stamped
	}
	root, oerr := os.OpenRoot(opts.MigrationsDir)
	if oerr != nil {
		return "", fmt.Errorf("open %s: %w", opts.MigrationsDir, oerr)
	}
	defer root.Close()
	temporary := ".gofastr-migration-" + rand.Text() + ".tmp"
	w, werr := root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if werr != nil {
		return "", fmt.Errorf("write %s: %w", path, werr)
	}
	defer root.Remove(temporary)
	if _, werr = w.Write([]byte(content)); werr != nil {
		w.Close()
		return "", fmt.Errorf("write %s: %w", path, werr)
	}
	if werr = w.Sync(); werr != nil {
		w.Close()
		return "", fmt.Errorf("sync %s: %w", path, werr)
	}
	if werr = w.Close(); werr != nil {
		return "", fmt.Errorf("write %s: %w", path, werr)
	}
	if err := root.Link(temporary, filename); err != nil {
		return "", fmt.Errorf("publish %s: %w", path, err)
	}
	if err := root.Remove(temporary); err != nil {
		return "", fmt.Errorf("remove migration temporary file: %w", err)
	}
	if err := atomicfile.SyncDir(opts.MigrationsDir); err != nil {
		return "", fmt.Errorf("sync migrations before snapshot update: %w", err)
	}
	if err := SaveSnapshot(snapPath, next); err != nil {
		return "", fmt.Errorf("snapshot update failed (migration written): %w", err)
	}
	return path, nil
}

// afterVersionScan is a test seam run between choosing the filename and
// creating the file.
var afterVersionScan func(dir, filename string)

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
