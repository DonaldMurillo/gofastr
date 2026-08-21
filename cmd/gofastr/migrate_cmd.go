package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func runMigrate(args []string) {
	if hasHelpFlag(args) {
		printMigrateUsage()
		return
	}
	// The subcommand is optional (see printHelp: "migrate (m) [up|down|…]"),
	// so a bare `gofastr migrate` must default to "up" without slicing past
	// the end of an empty arg vector (which panicked with slice bounds [1:0]).
	subcmd := "up"
	rest := args
	if len(args) > 0 {
		subcmd = args[0]
		rest = args[1:]
	}

	switch subcmd {
	case "up":
		runMigrateUp(rest)
	case "down":
		runMigrateDown(rest)
	case "status":
		runMigrateStatus(rest)
	case "diff":
		// Removed: `migrate diff` *applied* a blueprint onto a live DB,
		// reconciling the running database to the blueprint, i.e. treating
		// the blueprint as authoritative over the world. Schema migration is a
		// separate concern from code generation: emit a reviewable, versioned
		// migration instead. See framework/ARCHITECTURE.md.
		fail("`gofastr migrate diff` has been removed.")
		info("It applied a blueprint directly onto a live DB. Use `gofastr migrate generate <name>` to emit a reviewable migration, then `gofastr migrate up`. See `gofastr docs migrations`.")
		osExit(1)
	case "generate":
		runMigrateGenerate(rest)
	case "force":
		runMigrateForce(rest)
	default:
		fail("Unknown migrate subcommand: %q", subcmd)
		info("Available: up, down, status, generate, force")
		info("Usage: gofastr migrate [up|down|status|generate|force]")
		osExit(1)
	}
}

// runMigrateForce reconciles the tracking table by hand: the recovery path out
// of a dirty state and the baseline path for adopting an existing database.
//
//	gofastr migrate force <version> [--not-applied]
//
// By default the version is marked cleanly applied (clearing any dirty flag and
// recording a baseline without running its Up SQL). With --not-applied the
// version is removed from the tracking table so it becomes pending again.
func runMigrateForce(args []string) {
	applied := true
	var version uint64
	haveVersion := false
	for _, a := range args {
		switch {
		case a == "--not-applied":
			applied = false
		case strings.HasPrefix(a, "--"):
			// flag consumed elsewhere (e.g. --db-url): skip
		default:
			if _, err := fmt.Sscanf(a, "%d", &version); err == nil {
				haveVersion = true
			}
		}
	}
	if !haveVersion {
		fail("Usage: gofastr migrate force <version> [--not-applied]")
		osExit(1)
	}

	fmt.Printf("\n  %s\n\n", bold(fmt.Sprintf("Forcing migration %d → applied=%t...", version, applied)))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		fail("%v", err)
		osExit(1)
	}
	defer closeDB()

	if err := migrator.Force(context.Background(), version, applied, getGroups(args)...); err != nil {
		fail("Force failed: %v", err)
		osExit(1)
	}
	success("Tracking table reconciled for version %d", version)
}

func runMigrateUp(args []string) {
	fmt.Printf("\n  %s\n\n", bold("Running migrations..."))

	// --create-db: create the target database first if it doesn't exist.
	if hasFlag(args, "--create-db") {
		driver := getMigrateDriver(args)
		dbURL := getMigrateDBURL(args)
		if err := ensureDriverRegistered(driver); err != nil {
			fail("%v", err)
			osExit(1)
		}
		created, err := migrate.EnsureDatabase(driver, dbURL)
		if err != nil {
			fail("Could not ensure database exists: %v", err)
			osExit(1)
		}
		if created {
			success("Created database")
		}
	}

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		fail("%v", err)
		osExit(1)
	}
	defer closeDB()

	if err := migrator.Up(context.Background(), getGroups(args)...); err != nil {
		fail("Migration failed: %v", err)
		osExit(1)
	}
	success("Migrations applied")
}

func runMigrateDown(args []string) {
	n := 1
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			continue
		}
		fmt.Sscanf(a, "%d", &n)
	}

	fmt.Printf("\n  %s\n\n", bold("Rolling back migrations..."))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		fail("%v", err)
		osExit(1)
	}
	defer closeDB()

	if err := migrator.Down(context.Background(), n, getGroups(args)...); err != nil {
		fail("Rollback failed: %v", err)
		osExit(1)
	}
	success("Rollback complete")
}

func runMigrateStatus(args []string) {
	fmt.Printf("\n  %s\n\n", bold("Migration Status"))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		fail("%v", err)
		osExit(1)
	}
	defer closeDB()

	status, err := migrator.Status(context.Background(), getGroups(args)...)
	if err != nil {
		fail("Could not read migration status: %v", err)
		osExit(1)
	}

	// Show the group column only when a non-default group is present, so the
	// common all-default output stays byte-identical.
	showGroup := false
	for _, rec := range status.Applied {
		if rec.Group != "" {
			showGroup = true
			break
		}
	}
	if !showGroup {
		for _, mig := range status.Pending {
			if mig.Group != "" {
				showGroup = true
				break
			}
		}
	}

	fmt.Printf("    Applied: %d\n", len(status.Applied))
	fmt.Printf("    Pending: %d\n", len(status.Pending))
	for _, pending := range status.Pending {
		if showGroup {
			fmt.Printf("    %s %d %s [%s]\n", yellow("→"), pending.Version, pending.Name, groupLabel(pending.Group))
		} else {
			fmt.Printf("    %s %d %s\n", yellow("→"), pending.Version, pending.Name)
		}
	}
	for _, rec := range status.Applied {
		if rec.Dirty {
			if showGroup {
				// groupLabel maps the default group to "default", which the
				// CLI accepts as its alias; the printed command is runnable
				// verbatim for every group (a raw "" would render --group=,
				// which getGroups rejects).
				fmt.Printf("    %s %d %s [%s]: DIRTY (failed mid-apply; run `gofastr migrate force %d --group=%s` after reconciling)\n",
					yellow("⚠"), rec.Version, rec.Name, groupLabel(rec.Group), rec.Version, groupLabel(rec.Group))
			} else {
				fmt.Printf("    %s %d %s: DIRTY (failed mid-apply; run `gofastr migrate force %d` after reconciling)\n",
					yellow("⚠"), rec.Version, rec.Name, rec.Version)
			}
		}
	}
}

func migratorFromArgs(args []string) (*migrate.Migrator, func(), error) {
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("directory 'migrations/' not found.\n" +
			"This subcommand reads file-based migrations (migrations/*.sql), but apps scaffolded by `gofastr init` register migrations inline in Go (entities/entities.go → RegisterMigrations) and apply them at boot: there may be nothing on disk to read.\n" +
			"Review inline migrations in your entities package; to adopt file-based migrations, generate one with `gofastr migrate generate <name> --from=<blueprint.yml>` (see `gofastr docs migrations`).")
	}
	dbURL := getMigrateDBURL(args)
	if dbURL == "" {
		return nil, nil, fmt.Errorf("database URL is required; set DATABASE_URL or pass --db-url=<url>")
	}
	driver := getMigrateDriver(args)
	if err := ensureDriverRegistered(driver); err != nil {
		return nil, nil, err
	}
	db, err := sql.Open(driver, dbURL)
	if err != nil {
		return nil, nil, err
	}
	dialect := dialectForDriver(driver)
	migrator := migrate.New(db, migrate.WithDialect(dialect))
	if err := loadMigrationFiles(migrator, "migrations"); err != nil {
		db.Close()
		return nil, nil, err
	}
	return migrator, func() { _ = db.Close() }, nil
}

// dialectForDriver maps a database/sql driver name to a migration dialect.
// Both the CGO mattn/go-sqlite3 driver ("sqlite3") and the pure-Go engine
// ("sqlite", registered by core/sqlite) are SQLite. Matching on "sqlite3"
// alone left the pure-Go driver defaulting to the Postgres dialect, so
// `migrate up` silently ran Postgres SQL against a SQLite database.
func dialectForDriver(driver string) migrate.Dialect {
	if driver == "sqlite" || driver == "sqlite3" {
		return migrate.DialectSQLite
	}
	return migrate.DialectPostgres
}

func loadMigrationFiles(migrator *migrate.Migrator, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		err = migrator.RegisterFromReader(file)
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func getMigrateDBURL(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--db-url=") {
			return strings.TrimPrefix(a, "--db-url=")
		}
	}
	// The exported env var is the 12-factor path the docs promise.
	// Precedence follows the framework's dotenv rule: the process
	// environment always beats file values.
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	// Check the .env files via the shared parser: handles quoted values,
	// escapes, and the `export` prefix that the prior ad-hoc 1-key
	// scanner mishandled.
	//
	// framework.DefaultDotEnvPaths() rather than ".env" alone: that is the same set
	// and order NewApp loads, and Load resolves conflicts earliest-file-wins.
	// Reading ".env" alone ignored .env.local, the HIGHEST-precedence file,
	// and the documented place to override a local database, so `gofastr
	// migrate` migrated a different database than the app it was migrating
	// for would open.
	if vals, err := dotenv.Load(framework.DefaultDotEnvPaths()...); err == nil {
		if v, ok := vals["DATABASE_URL"]; ok {
			return v
		}
	}
	return ""
}

// ensureDriverRegistered fails fast with a useful message if the requested
// database/sql driver is not blank-imported into the binary. The default CLI
// build only links sqlite3; users who need postgres or mysql build their own
// gofastr binary that imports the appropriate driver.
func ensureDriverRegistered(driver string) error {
	for _, registered := range sql.Drivers() {
		if registered == driver {
			return nil
		}
	}
	return fmt.Errorf("driver %q is not registered (available: %v). Build a gofastr CLI that blank-imports the driver you need (e.g. _ \"github.com/lib/pq\")", driver, sql.Drivers())
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func getMigrateDriver(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--driver=") {
			return strings.TrimPrefix(a, "--driver=")
		}
	}
	return "sqlite3"
}

// getGroups extracts repeatable --group=<name> flags from args. Returns nil
// when no --group is present (the no-args "all groups" default for up/down/
// status, and the "default group" target for force).
func getGroups(args []string) []string {
	var groups []string
	for _, a := range args {
		if strings.HasPrefix(a, "--group=") {
			val := strings.TrimPrefix(a, "--group=")
			if val == "" {
				fail("--group= requires a non-empty group name")
				osExit(1)
			}
			groups = append(groups, val)
		}
	}
	return groups
}

// groupLabel renders a group name for status output, showing "default" for the
// empty (default) group.
func groupLabel(g string) string {
	if g == "" {
		return "default"
	}
	return g
}
