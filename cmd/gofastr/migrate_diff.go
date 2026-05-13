package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework"
)

// runMigrateDiff prints the ALTER TABLE statements needed to reconcile the
// live DB with the entity declarations under ./entities. With --apply, runs
// the changes inside a single transaction.
//
//	gofastr migrate diff [--db-url=...] [--entities=...] [--apply]
//
// Without --db-url, falls back to DATABASE_URL.
func runMigrateDiff(args []string) {
	opts := parseDiffOptions(args)

	decls, err := framework.LoadEntityDeclarations(opts.entitiesDir)
	if err != nil {
		fail("Failed to load entity declarations: %v", err)
		os.Exit(1)
	}
	if len(decls) == 0 {
		fail("No entity declarations found in %s.", opts.entitiesDir)
		os.Exit(1)
	}

	db, err := openDiffDB(opts.dbURL, opts.driver)
	if err != nil {
		fail("%v", err)
		os.Exit(1)
	}
	defer db.Close()

	// Build a transient Registry from the declarations so DiffSchema can
	// inspect entity-level config (timestamps, soft-delete, multi-tenant).
	reg := framework.NewRegistry()
	for _, decl := range decls {
		cfg, err := decl.Config()
		if err != nil {
			fail("entity %q: %v", decl.Name, err)
			os.Exit(1)
		}
		reg.Register(framework.Define(decl.Name, cfg))
	}

	changes, err := framework.DiffSchema(context.Background(), db, reg)
	if err != nil {
		fail("DiffSchema failed: %v", err)
		os.Exit(1)
	}
	if len(changes) == 0 {
		success("Schema is up to date — no changes needed.")
		return
	}

	fmt.Printf("\n  %s\n\n", bold(fmt.Sprintf("%d change(s):", len(changes))))
	for _, c := range changes {
		fmt.Printf("  → %s\n", c.Summary)
		for _, line := range strings.Split(strings.TrimSpace(c.SQL), "\n") {
			fmt.Printf("      %s\n", line)
		}
	}
	fmt.Println()

	if !opts.apply {
		info("Re-run with --apply to execute these in a single transaction.")
		return
	}

	n, err := framework.ApplySchemaDiff(context.Background(), db, changes)
	if err != nil {
		fail("Apply failed at change %d: %v", n+1, err)
		os.Exit(1)
	}
	success("Applied %d change(s).", n)
}

type diffOptions struct {
	dbURL       string
	driver      string
	entitiesDir string
	apply       bool
}

func parseDiffOptions(args []string) diffOptions {
	opts := diffOptions{
		entitiesDir: "entities",
		driver:      "sqlite3",
	}
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "--db-url="):
			opts.dbURL = strings.TrimPrefix(arg, "--db-url=")
		case strings.HasPrefix(arg, "--driver="):
			opts.driver = strings.TrimPrefix(arg, "--driver=")
		case strings.HasPrefix(arg, "--entities="):
			opts.entitiesDir = strings.TrimPrefix(arg, "--entities=")
		case arg == "--apply":
			opts.apply = true
		}
	}
	if opts.dbURL == "" {
		opts.dbURL = os.Getenv("DATABASE_URL")
	}
	return opts
}

// openDiffDB picks a driver based on the URL scheme (postgres://* → postgres,
// otherwise sqlite3). Mirrors the migrate up/down path's lookup.
func openDiffDB(dbURL, driver string) (*sql.DB, error) {
	if dbURL == "" {
		return nil, fmt.Errorf("database URL is required; set DATABASE_URL or pass --db-url=<url>")
	}
	if strings.HasPrefix(dbURL, "postgres://") || strings.HasPrefix(dbURL, "postgresql://") {
		driver = "postgres"
	}
	db, err := sql.Open(driver, dbURL)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", driver, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return db, nil
}
