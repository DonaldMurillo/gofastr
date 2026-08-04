package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/migrate"
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
	if opts.group != "" {
		if err := migrate.ValidateGroupName(opts.group); err != nil {
			fail("%v", err)
			osExit(1)
		}
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

	// Both generation paths — this CLI and a host binary calling
	// framework.GenerateMigrationFile with its own compiled registry — go
	// through one implementation, so the file naming, directive layout and
	// snapshot format cannot drift apart.
	path, err := framework.GenerateMigrationFile(
		framework.MigrationPlan{Registry: reg},
		opts.name,
		framework.MigrationFileOptions{
			MigrationsDir: opts.migrationsDir,
			SnapshotPath:  opts.snapshotPath,
			Dialect:       dialect,
			Group:         opts.group,
		},
	)
	if err != nil {
		fail("Generate failed: %v", err)
		osExit(1)
	}
	if path == "" {
		success("Schema is up to date — nothing to generate.")
		return
	}

	success("Generated %s", path)
	info("Review it, commit it, then run `gofastr migrate up`.")
	if body, err := os.ReadFile(path); err == nil &&
		!strings.Contains(string(body), "-- +migrate Down") {
		info("Note: this migration has no Down section (no safe inverse) — it is not reversible.")
	}
}

type migrateGenOptions struct {
	name          string
	from          string
	migrationsDir string
	snapshotPath  string
	driver        string
	group         string
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
		case strings.HasPrefix(arg, "--group="):
			opts.group = strings.TrimPrefix(arg, "--group=")
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
