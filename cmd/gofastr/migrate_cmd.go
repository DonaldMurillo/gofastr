package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gofastr/gofastr/core/migrate"
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
	default:
		fail("Unknown migrate subcommand: %q", subcmd)
		info("Available: up, down, status, diff")
		info("Usage: gofastr migrate [up|down|status|diff]")
		os.Exit(1)
	}
}

func runMigrateUp(args []string) {
	fmt.Printf("\n  %s\n\n", bold("Running migrations..."))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		fail("%v", err)
		os.Exit(1)
	}
	defer closeDB()

	if err := migrator.Up(context.Background()); err != nil {
		fail("Migration failed: %v", err)
		os.Exit(1)
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
		os.Exit(1)
	}
	defer closeDB()

	if err := migrator.Down(context.Background(), n); err != nil {
		fail("Rollback failed: %v", err)
		os.Exit(1)
	}
	success("Rollback complete")
}

func runMigrateStatus(args []string) {
	fmt.Printf("\n  %s\n\n", bold("Migration Status"))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		fail("%v", err)
		os.Exit(1)
	}
	defer closeDB()

	status, err := migrator.Status(context.Background())
	if err != nil {
		fail("Could not read migration status: %v", err)
		os.Exit(1)
	}

	fmt.Printf("    Applied: %d\n", len(status.Applied))
	fmt.Printf("    Pending: %d\n", len(status.Pending))
	for _, pending := range status.Pending {
		fmt.Printf("    %s %d %s\n", yellow("→"), pending.Version, pending.Name)
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
	// Check .env file
	if data, err := os.ReadFile(".env"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "DATABASE_URL=") {
				return strings.TrimPrefix(line, "DATABASE_URL=")
			}
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

func getMigrateDriver(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--driver=") {
			return strings.TrimPrefix(a, "--driver=")
		}
	}
	return "sqlite3"
}
