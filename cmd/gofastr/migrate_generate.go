package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
)

// runMigrateGenerate turns a change to the entity declarations into a reviewable,
// reversible, versioned migration file — the offline declarative workflow.
//
//	gofastr migrate generate <name> --from=<blueprint.yml> [--migrations=dir]
//	                                [--snapshot=path] [--driver=name]
//
// It diffs the blueprint's entities against a committed schema snapshot (no
// database needed), writes `migrations/NNNN_<name>.sql` with Up and Down
// sections, and updates the snapshot. Review the file, commit it, then
// `gofastr migrate up`.
func runMigrateGenerate(args []string) {
	opts := parseMigrateGenOptions(args)
	if opts.name == "" {
		fail("Usage: gofastr migrate generate <name> --from=<blueprint.yml> [--migrations=dir] [--snapshot=path] [--driver=name]")
		osExit(1)
	}
	if opts.from == "" {
		fail("A blueprint is required: pass --from=<blueprint.yml>.")
		osExit(1)
	}

	dialect := framework.DialectSQLite
	if opts.driver == "postgres" || opts.driver == "pgx" {
		dialect = framework.DialectPostgres
	}

	bp, err := loadBlueprint(opts.from)
	if err != nil {
		fail("Failed to load blueprint %s: %v", opts.from, err)
		osExit(1)
	}
	decls := bp.Entities
	if len(decls) == 0 {
		fail("Blueprint %s declares no entities.", opts.from)
		osExit(1)
	}
	reg := framework.NewRegistry()
	for _, decl := range decls {
		cfg, err := decl.Config()
		if err != nil {
			fail("entity %q: %v", decl.Name, err)
			osExit(1)
		}
		reg.Register(framework.Define(decl.Name, cfg))
	}

	prev, err := framework.LoadSnapshot(opts.snapshotPath)
	if err != nil {
		fail("Failed to read snapshot %s: %v", opts.snapshotPath, err)
		osExit(1)
	}

	up, down, next, err := framework.GenerateMigration(reg, prev, dialect)
	if err != nil {
		fail("Generate failed: %v", err)
		osExit(1)
	}
	if up == "" {
		success("Schema is up to date — nothing to generate.")
		return
	}

	if err := os.MkdirAll(opts.migrationsDir, 0o755); err != nil {
		fail("Could not create %s: %v", opts.migrationsDir, err)
		osExit(1)
	}
	version := nextMigrationVersion(opts.migrationsDir)
	slug := sanitizeMigrationName(opts.name)
	filename := fmt.Sprintf("%04d_%s.sql", version, slug)
	path := filepath.Join(opts.migrationsDir, filename)

	content := framework.RenderMigrationFile(version, slug, up, down)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fail("Could not write %s: %v", path, err)
		osExit(1)
	}
	if err := framework.SaveSnapshot(opts.snapshotPath, next); err != nil {
		fail("Migration written but snapshot update failed: %v", err)
		osExit(1)
	}

	success("Generated %s", path)
	info("Review it, commit it, then run `gofastr migrate up`.")
	if down == "" {
		info("Note: this migration has no Down section (no safe inverse) — it is not reversible.")
	}
}

type migrateGenOptions struct {
	name          string
	from          string
	migrationsDir string
	snapshotPath  string
	driver        string
}

func parseMigrateGenOptions(args []string) migrateGenOptions {
	opts := migrateGenOptions{
		migrationsDir: "migrations",
		driver:        "sqlite3",
	}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--from="):
			opts.from = strings.TrimPrefix(arg, "--from=")
		case strings.HasPrefix(arg, "--migrations="):
			opts.migrationsDir = strings.TrimPrefix(arg, "--migrations=")
		case strings.HasPrefix(arg, "--snapshot="):
			opts.snapshotPath = strings.TrimPrefix(arg, "--snapshot=")
		case strings.HasPrefix(arg, "--driver="):
			opts.driver = strings.TrimPrefix(arg, "--driver=")
		case strings.HasPrefix(arg, "--"):
			// unknown flag — ignore
		default:
			if opts.name == "" {
				opts.name = arg
			}
		}
	}
	if opts.snapshotPath == "" {
		opts.snapshotPath = filepath.Join(opts.migrationsDir, "schema.snapshot.json")
	}
	return opts
}

// nextMigrationVersion returns one past the highest NNNN_ prefix among the
// existing .sql files, or 1 when the directory is empty.
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
// single underscore so the name is filesystem- and directive-safe.
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
