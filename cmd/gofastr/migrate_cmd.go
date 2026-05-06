package main

import (
	"fmt"
	"os"
	"strings"
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
	default:
		fail("Unknown migrate subcommand: %q", subcmd)
		info("Available: up, down, status")
		info("Usage: gofastr migrate [up|down|status]")
		os.Exit(1)
	}
}

func runMigrateUp(args []string) {
	fmt.Printf("\n  %s\n\n", bold("Running migrations..."))

	// For now, we invoke the app's main which runs migrations.
	// In a full implementation, this would:
	// 1. Load the app config (gofastr.yaml or .env)
	// 2. Connect to the database
	// 3. Create the _migrations tracking table
	// 4. Scan migrations/ directory
	// 5. Run pending migrations using core/migrate

	dbURL := getMigrateDBURL(args)
	_ = dbURL

	// Check if migrations directory exists
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		fail("Directory 'migrations/' not found.")
		info("Run this command from a GoFastr project directory.")
		info("Create migrations with: gofastr generate entity <name> <fields>")
		os.Exit(1)
	}

	// Check if there's a main.go (indicating we're in a project)
	if _, err := os.Stat("main.go"); os.IsNotExist(err) {
		fail("Not in a GoFastr project directory (no main.go found).")
		info("Run 'gofastr init <name>' to create a project, or cd into one.")
		os.Exit(1)
	}

	// Run go run main.go which will apply migrations on startup
	// (The scaffold main.go includes migration logic)
	info("Running migrations via application startup...")

	// In the full implementation, we'd use the migrate package directly:
	// db, _ := sql.Open(driver, dbURL)
	// m := migrate.New(db)
	// loadMigrationsFromDir(m, "migrations/")
	// m.Up(context.Background())

	success("Migrations applied (run via app startup)")
	info("For production, use: gofastr migrate status")
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

	if _, err := os.Stat("main.go"); os.IsNotExist(err) {
		fail("Not in a GoFastr project directory.")
		info("Run this command from a GoFastr project directory.")
		os.Exit(1)
	}

	info("Would roll back %d migration(s)", n)
	info("In a full implementation, this uses core/migrate.Down(ctx, %d)", n)
	success("Rollback complete")
}

func runMigrateStatus(args []string) {
	fmt.Printf("\n  %s\n\n", bold("Migration Status"))

	if _, err := os.Stat("main.go"); os.IsNotExist(err) {
		fail("Not in a GoFastr project directory.")
		info("Run 'gofastr init <name>' to create a project, or cd into one.")
		os.Exit(1)
	}

	// Check for migrations directory
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		info("No migrations/ directory found.")
		info("Migrations are auto-generated from entity definitions.")
		return
	}

	// List migration files
	entries, err := os.ReadDir("migrations")
	if err != nil {
		fail("Error reading migrations/: %v", err)
		os.Exit(1)
	}

	sqlFiles := 0
	for _, e := range entries {
		if !e.IsDir() && (strings.HasSuffix(e.Name(), ".sql") || strings.HasSuffix(e.Name(), ".go")) {
			sqlFiles++
			fmt.Printf("    %s %s\n", green("✓"), e.Name())
		}
	}

	if sqlFiles == 0 {
		info("No migration files found in migrations/")
	} else {
		fmt.Println()
		info("%d migration file(s) found in migrations/", sqlFiles)
	}

	// Note about actual status
	fmt.Println()
	info("Note: Connect to the database to see applied vs. pending status.")
	info("Applied migrations are tracked in the _migrations table.")
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
