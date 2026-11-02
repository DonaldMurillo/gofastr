package migratecmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/dotenv"
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/framework"
	fwmigrate "github.com/DonaldMurillo/gofastr/framework/migrate"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func RunMigrate(args []string) {
	if cli.HasHelpFlag(args) {
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

	if err := migrateFlagsError(subcmd, rest); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	switch subcmd {
	case "up":
		runMigrateUp(rest)
	case "down":
		runMigrateDown(rest)
	case "status":
		runMigrateStatus(rest)
	case "diff":
		// Removed: `migrate diff` *applied* an app spec onto a live DB,
		// reconciling the running database to the app spec, i.e. treating
		// the app spec as authoritative over the world. Schema migration is a
		// separate concern from code generation: emit a reviewable, versioned
		// migration instead. See framework/ARCHITECTURE.md.
		cli.Fail("`gofastr migrate diff` has been removed.")
		cli.Info("It applied an app spec directly onto a live DB. Use `gofastr migrate generate <name>` to emit a reviewable migration, then `gofastr migrate up`. See `gofastr docs migrations`.")
		// Exit non-zero: a deploy script that still runs `migrate diff`
		// must stop here, not carry on as if the schema were reconciled.
		cli.OsExit(1)
	case "generate":
		runMigrateGenerate(rest)
	case "force":
		runMigrateForce(rest)
	case "repair":
		runMigrateRepair(rest)
	default:
		cli.Fail("Unknown migrate subcommand: %q", subcmd)
		cli.Info("Available: up, down, status, generate, force, repair")
		cli.Info("Usage: gofastr migrate [up|down|status|generate|force|repair]")
		cli.OsExit(1)
	}
}

// migrateFlags names the flags each subcommand reads: valued ones take
// --name=<value>, bare ones nothing. The readers match by prefix and
// skip the rest, so a typo was dropped in silence: --db-ur=<url> ran
// against DATABASE_URL, and --db-url <url> (the space form) did too.
var migrateFlags = map[string]struct{ valued, bare []string }{
	"up":       {[]string{"--db-url", "--db", "--driver", "--group"}, []string{"--create-db"}},
	"down":     {[]string{"--db-url", "--db", "--driver", "--group"}, nil},
	"status":   {[]string{"--db-url", "--db", "--driver", "--group"}, nil},
	"force":    {[]string{"--db-url", "--db", "--driver", "--group"}, []string{"--not-applied"}},
	"repair":   {[]string{"--db-url", "--db", "--driver", "--from"}, []string{"--apply"}},
	"generate": {[]string{"--db", "--driver", "--from", "--group", "--migrations", "--snapshot"}, nil},
}

// migrateFlagsError refuses a flag the subcommand does not read, a valued
// flag written without =<value>, and a bare flag given one.
func migrateFlagsError(subcmd string, args []string) error {
	known, ok := migrateFlags[subcmd]
	if !ok {
		return nil
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name, _, hasValue := strings.Cut(a, "=")
		switch {
		case slices.Contains(known.valued, name):
			if !hasValue {
				return fmt.Errorf("migrate %s: write %s=<value>", subcmd, name)
			}
		case slices.Contains(known.bare, name):
			if hasValue {
				return fmt.Errorf("migrate %s: %s takes no value", subcmd, name)
			}
		default:
			return fmt.Errorf("migrate %s: unknown flag %s", subcmd, name)
		}
	}
	return nil
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
	// Exactly one version, parsed whole. Sscanf read "1x" as 1, and with
	// two versions the last one won, so a typo marked a migration applied
	// without running its SQL.
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
			v, err := strconv.ParseUint(a, 10, 64)
			if err != nil || haveVersion {
				cli.Fail("migrate force takes exactly one migration version, got %q", a)
				cli.OsExit(1)
				return
			}
			version, haveVersion = v, true
		}
	}
	if !haveVersion {
		cli.Fail("Usage: gofastr migrate force <version> [--not-applied]")
		cli.OsExit(1)
	}

	fmt.Printf("\n  %s\n\n", cli.Bold(fmt.Sprintf("Forcing migration %d → applied=%t...", version, applied)))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
	defer closeDB()

	if err := migrator.Force(context.Background(), version, applied, getGroups(args)...); err != nil {
		cli.Fail("Force failed: %v", err)
		cli.OsExit(1)
	}
	cli.Success("Tracking table reconciled for version %d", version)
}

// runMigrateRepair reports — and with --apply rebuilds — SQLite tables still
// carrying the pre-v0.67 foreign key on an entity's owner column.
//
//	gofastr migrate repair --from=<gofastr.yml> [--apply]
//
// The scan needs each entity's owner column, so it reads the app spec the
// same way `migrate generate` does. Without --apply it prints each finding
// and exits non-zero (0 when the database is clean), so a release check can
// gate on it. With --apply it names every table it is about to rebuild
// before rewriting anything, repairs, then re-scans to confirm the database
// is clean — a repair that reports success while leaving a key behind is
// worse than no repair, because the operator stops looking.
func runMigrateRepair(args []string) {
	fmt.Printf("\n  %s\n\n", cli.Bold("Scanning for stale owner-column foreign keys..."))

	from := ""
	for _, a := range args {
		if strings.HasPrefix(a, "--from=") {
			from = strings.TrimPrefix(a, "--from=")
		}
	}
	if from == "" {
		cli.Fail("Usage: gofastr migrate repair --from=<gofastr.yml> [--apply] [--db-url=<url>]")
		cli.Info("The scan reads the entity declarations to know each table's owner column.")
		cli.OsExit(1)
	}
	dbURL := getMigrateDBURL(args)
	if dbURL == "" {
		cli.Fail("database URL is required; set DATABASE_URL or pass --db-url=<url>")
		cli.OsExit(1)
	}
	driver := getMigrateDriver(args)
	if err := ensureDriverRegistered(driver); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
	db, err := sql.Open(driver, dbURL)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
	defer func() { _ = db.Close() }()

	reg := framework.NewRegistry()
	bp, err := appspec.LoadAppSpec(from)
	if err != nil {
		cli.Fail("Failed to load app spec %s: %v", from, err)
		cli.OsExit(1)
	}
	if len(bp.Entities) == 0 {
		cli.Fail("App spec %s declares no entities.", from)
		cli.OsExit(1)
	}
	for _, decl := range bp.Entities {
		cfg, err := decl.Config()
		if err != nil {
			cli.Fail("entity %q: %v", decl.Name, err)
			cli.OsExit(1)
		}
		if err := reg.Register(framework.Define(decl.Name, cfg)); err != nil {
			cli.Fail("entity %q: %v", decl.Name, err)
			cli.OsExit(1)
		}
	}

	stale, err := fwmigrate.FindStaleOwnerForeignKeys(context.Background(), db, reg)
	if err != nil {
		cli.Fail("Scan failed: %v", err)
		cli.OsExit(1)
	}
	if len(stale) == 0 {
		cli.Success("No stale owner-column foreign keys")
		return
	}
	for _, s := range stale {
		fmt.Printf("    %s %s\n", cli.Yellow("⚠"), s)
	}

	if !hasFlag(args, "--apply") {
		cli.Info("Report only. Re-run with --apply to rebuild the listed tables without the stale key.")
		cli.Info("The rebuild keeps every row, column, default, index and other constraint.")
		cli.Info("The scan reads the schema only. A key whose referenced table really does hold the stamped owner ids is satisfiable, and --apply would drop it. Check the listed tables first.")
		cli.OsExit(1)
	}

	fmt.Printf("\n  %s\n\n", cli.Bold("Rebuilding..."))
	for _, s := range stale {
		fmt.Printf("    %s rewriting %s without the stale key\n", cli.Yellow("→"), s.Table)
	}
	if err := fwmigrate.RepairStaleOwnerForeignKeys(context.Background(), db, stale); err != nil {
		cli.Fail("Repair failed: %v", err)
		cli.OsExit(1)
	}
	after, err := fwmigrate.FindStaleOwnerForeignKeys(context.Background(), db, reg)
	if err != nil {
		cli.Fail("Re-scan failed after repair: %v", err)
		cli.OsExit(1)
	}
	if len(after) > 0 {
		cli.Fail("Repair reported success but the re-scan still finds: %v", after)
		cli.OsExit(1)
	}
	cli.Success("Rebuilt %d table(s); re-scan clean", len(stale))
}

func runMigrateUp(args []string) {
	fmt.Printf("\n  %s\n\n", cli.Bold("Running migrations..."))

	// --create-db: create the target database first if it doesn't exist.
	if hasFlag(args, "--create-db") {
		// Check the arguments first: a run refused after the create left
		// an empty database behind.
		if err := migrateArgsError(args); err != nil {
			cli.Fail("%v", err)
			cli.OsExit(1)
			return
		}
		driver := getMigrateDriver(args)
		dbURL := getMigrateDBURL(args)
		if err := ensureDriverRegistered(driver); err != nil {
			cli.Fail("%v", err)
			cli.OsExit(1)
		}
		created, err := migrate.EnsureDatabase(driver, dbURL)
		if err != nil {
			cli.Fail("Could not ensure database exists: %v", err)
			cli.OsExit(1)
		}
		if created {
			cli.Success("Created database")
		}
	}

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
	defer closeDB()

	if err := migrator.Up(context.Background(), getGroups(args)...); err != nil {
		cli.Fail("Migration failed: %v", err)
		cli.OsExit(1)
	}
	cli.Success("Migrations applied")
}

func runMigrateDown(args []string) {
	// The count is a positive integer or absent. Sscanf read "all" as no
	// number and "3x" as 3, so a typo rolled back one or three. With two
	// counts the last one won, so `down 1 3` dropped three tables: one
	// count at most, as force takes one version.
	n := 1
	haveCount := false
	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			continue
		}
		v, err := strconv.Atoi(a)
		if err != nil || v < 1 || haveCount {
			cli.Fail("migrate down takes at most one positive number of migrations to roll back, got %q", a)
			cli.OsExit(1)
			return
		}
		n, haveCount = v, true
	}

	fmt.Printf("\n  %s\n\n", cli.Bold("Rolling back migrations..."))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
	defer closeDB()

	if err := migrator.Down(context.Background(), n, getGroups(args)...); err != nil {
		cli.Fail("Rollback failed: %v", err)
		cli.OsExit(1)
	}
	cli.Success("Rollback complete")
}

func runMigrateStatus(args []string) {
	fmt.Printf("\n  %s\n\n", cli.Bold("Migration Status"))

	migrator, closeDB, err := migratorFromArgs(args)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}
	defer closeDB()

	status, err := migrator.Status(context.Background(), getGroups(args)...)
	if err != nil {
		cli.Fail("Could not read migration status: %v", err)
		cli.OsExit(1)
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
			fmt.Printf("    %s %d %s [%s]\n", cli.Yellow("→"), pending.Version, pending.Name, groupLabel(pending.Group))
		} else {
			fmt.Printf("    %s %d %s\n", cli.Yellow("→"), pending.Version, pending.Name)
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
					cli.Yellow("⚠"), rec.Version, rec.Name, groupLabel(rec.Group), rec.Version, groupLabel(rec.Group))
			} else {
				fmt.Printf("    %s %d %s: DIRTY (failed mid-apply; run `gofastr migrate force %d` after reconciling)\n",
					cli.Yellow("⚠"), rec.Version, rec.Name, rec.Version)
			}
		}
	}
}

// migrateArgsError is what migratorFromArgs refuses before it opens
// anything: no migrations directory, or no database URL.
func migrateArgsError(args []string) error {
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		return fmt.Errorf("directory 'migrations/' not found.\n" +
			"This subcommand reads file-based migrations (migrations/*.sql), but apps scaffolded by `gofastr init` register migrations inline in Go (entities/entities.go → RegisterMigrations) and apply them at boot: there may be nothing on disk to read.\n" +
			"Review inline migrations in your entities package; to adopt file-based migrations, generate one with `gofastr migrate generate <name> --from=<gofastr.yml>` (see `gofastr docs migrations`).")
	}
	if getMigrateDBURL(args) == "" {
		return fmt.Errorf("database URL is required; set DATABASE_URL or pass --db-url=<url>")
	}
	return ensureDriverRegistered(getMigrateDriver(args))
}

func migratorFromArgs(args []string) (*migrate.Migrator, func(), error) {
	if err := migrateArgsError(args); err != nil {
		return nil, nil, err
	}
	dbURL := getMigrateDBURL(args)
	driver := getMigrateDriver(args)
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

// getMigrateDriver resolves the driver from --db= (the spelling
// `gofastr migrate --help` and `gofastr init` use) or its older alias
// --driver=, through canonicalDBDriver. Before --db= was read here,
// `migrate --db=postgres` fell through to the sqlite3 default and
// created a SQLite file named after the Postgres URL.
func getMigrateDriver(args []string) string {
	for _, a := range args {
		if v, ok := migrateDriverFlag(a); ok {
			return project.CanonicalDBDriver(v)
		}
	}
	return "sqlite3"
}

// migrateDriverFlag reports the value of a --db= or --driver= argument.
func migrateDriverFlag(arg string) (string, bool) {
	if v, ok := strings.CutPrefix(arg, "--db="); ok {
		return v, true
	}
	return strings.CutPrefix(arg, "--driver=")
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
				cli.Fail("--group= requires a non-empty group name")
				cli.OsExit(1)
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
