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
	_ "github.com/mattn/go-sqlite3"
)

func runMigrate(args []string) {
	subcmd := "up"
	if len(args) > 0 {
		subcmd = args[0]
	}

	switch subcmd {
	case "up":
		runMigrateUp(args[1:])
	case "down":
		runMigrateDown(args[1:])
	case "status":
		runMigrateStatus(args[1:])
	case "diff":
		runMigrateDiff(args[1:])
	case "generate":
		runMigrateGenerate(args[1:])
	case "force":
		runMigrateForce(args[1:])
	default:
		fail("Unknown migrate subcommand: %q", subcmd)
		info("Available: up, down, status, diff, generate, force")
		info("Usage: gofastr migrate [up|down|status|diff|generate|force]")
		osExit(1)
	}
}

// runMigrateForce reconciles the tracking table by hand — the recovery path out
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
			// flag consumed elsewhere (e.g. --db-url) — skip
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

	if err := migrator.Force(context.Background(), version, applied); err != nil {
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

	if err := migrator.Up(context.Background()); err != nil {
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

	if err := migrator.Down(context.Background(), n); err != nil {
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

	status, err := migrator.Status(context.Background())
	if err != nil {
		fail("Could not read migration status: %v", err)
		osExit(1)
	}

	fmt.Printf("    Applied: %d\n", len(status.Applied))
	fmt.Printf("    Pending: %d\n", len(status.Pending))
	for _, pending := range status.Pending {
		fmt.Printf("    %s %d %s\n", yellow("→"), pending.Version, pending.Name)
	}
	for _, rec := range status.Applied {
		if rec.Dirty {
			fmt.Printf("    %s %d %s — DIRTY (failed mid-apply; run `gofastr migrate force %d` after reconciling)\n",
				yellow("⚠"), rec.Version, rec.Name, rec.Version)
		}
	}
}

func migratorFromArgs(args []string) (*migrate.Migrator, func(), error) {
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("directory 'migrations/' not found")
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
	dialect := migrate.DialectPostgres
	if driver == "sqlite3" {
		dialect = migrate.DialectSQLite
	}
	migrator := migrate.New(db, migrate.WithDialect(dialect))
	if err := loadMigrationFiles(migrator, "migrations"); err != nil {
		db.Close()
		return nil, nil, err
	}
	return migrator, func() { _ = db.Close() }, nil
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
	// Check .env file via the shared parser — handles quoted values,
	// escapes, and the `export` prefix that the prior ad-hoc 1-key
	// scanner mishandled.
	if vals, err := dotenv.Load(".env"); err == nil {
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
	return fmt.Errorf("driver %q is not registered (available: %v) — build a gofastr CLI that blank-imports the driver you need (e.g. _ \"github.com/lib/pq\")", driver, sql.Drivers())
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
