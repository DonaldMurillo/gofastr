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

// RunMigrate implements `gofastr migrate`, up when no subcommand is given.
func RunMigrate(args []string) {
	if cli.HasHelpFlag(args) {
		printMigrateUsage()
		return
	}
	subcmd := "up"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcmd = args[0]
		rest = args[1:]
	}

	if err := migrateFlagsError(subcmd, rest); err != nil {
		cli.UsageFail("%v", err)
		return
	}
	if err := migratePositionalError(subcmd, rest); err != nil {
		cli.Fail("%v", err)
		printMigrateUsage()
		cli.OsExit(2)
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
		cli.Fail("`gofastr migrate diff` has been removed.")
		cli.Info("It applied an app spec directly onto a live DB. Use `gofastr migrate generate <name>` to emit a reviewable migration, then `gofastr migrate up`. See `gofastr docs migrations`.")
		cli.OsExit(1)
	case "generate":
		runMigrateGenerate(rest)
	case "force":
		runMigrateForce(rest)
	case "repair":
		runMigrateRepair(rest)
	default:
		cli.UsageFail("Unknown migrate subcommand: %q. Available: up, down, status, generate, force, repair.", subcmd)
	}
}

var migrateFlags = map[string]struct{ valued, bare []string }{
	"up":       {[]string{"--db-url", "--db", "--driver", "--group"}, []string{"--create-db"}},
	"down":     {[]string{"--db-url", "--db", "--driver", "--group"}, nil},
	"status":   {[]string{"--db-url", "--db", "--driver", "--group"}, nil},
	"force":    {[]string{"--db-url", "--db", "--driver", "--group"}, []string{"--not-applied"}},
	"repair":   {[]string{"--db-url", "--db", "--driver", "--from"}, []string{"--apply"}},
	"generate": {[]string{"--db", "--driver", "--from", "--group", "--migrations", "--snapshot"}, nil},
}

var migratePositional = map[string]int{
	"up": 0, "status": 0, "repair": 0, "down": 1, "force": 1, "generate": 1,
}

func migratePositionalError(subcmd string, args []string) error {
	limit, ok := migratePositional[subcmd]
	if !ok {
		return nil
	}
	var extra []string
	n := 0
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		n++
		if n > limit {
			extra = append(extra, a)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	if limit == 0 {
		return fmt.Errorf("migrate %s takes no arguments, got %q", subcmd, extra)
	}
	return fmt.Errorf("migrate %s takes one argument, got extra %q", subcmd, extra)
}

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

func runMigrateForce(args []string) {
	applied := true
	var version uint64
	haveVersion := false
	for _, a := range args {
		switch {
		case a == "--not-applied":
			applied = false
		case strings.HasPrefix(a, "--"):
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
		cli.UsageFail("Usage: gofastr migrate force <version> [--not-applied]")
	}

	cli.Printf("\n  "+cli.Bold("Forcing migration %d → applied=%t...")+"\n\n", version, applied)

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
		cli.OsExit(2)
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
	if err := registerSpecEntities(reg, bp.Entities); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
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
		cli.Printf("    "+cli.Yellow("⚠")+" %s\n", s)
	}

	if !hasFlag(args, "--apply") {
		cli.Info("Report only. Re-run with --apply to rebuild the listed tables without the stale key.")
		cli.Info("The rebuild keeps every row, column, default, index and other constraint.")
		cli.Info("The scan reads the schema only. A key whose referenced table really does hold the stamped owner ids is satisfiable, and --apply would drop it. Check the listed tables first.")
		cli.OsExit(1)
	}

	fmt.Printf("\n  %s\n\n", cli.Bold("Rebuilding..."))
	for _, s := range stale {
		cli.Printf("    "+cli.Yellow("→")+" rewriting %s without the stale key\n", s.Table)
	}
	if err := repairStaleOwnerKeys(context.Background(), db, stale); err != nil {
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

var repairStaleOwnerKeys = fwmigrate.RepairStaleOwnerForeignKeys

func runMigrateUp(args []string) {
	fmt.Printf("\n  %s\n\n", cli.Bold("Running migrations..."))

	if hasFlag(args, "--create-db") {
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
			cli.Printf("    "+cli.Yellow("→")+" %d %s [%s]\n", pending.Version, pending.Name, groupLabel(pending.Group))
		} else {
			cli.Printf("    "+cli.Yellow("→")+" %d %s\n", pending.Version, pending.Name)
		}
	}
	for _, rec := range status.Applied {
		if rec.Dirty {
			if showGroup {
				cli.Printf("    "+cli.Yellow("⚠")+" %d %s [%s]: DIRTY (failed mid-apply; run `gofastr migrate force %d --group=%s` after reconciling)\n",
					rec.Version, rec.Name, groupLabel(rec.Group), rec.Version, groupLabel(rec.Group))
			} else {
				cli.Printf("    "+cli.Yellow("⚠")+" %d %s: DIRTY (failed mid-apply; run `gofastr migrate force %d` after reconciling)\n",
					rec.Version, rec.Name, rec.Version)
			}
		}
	}
	label := func(rec migrate.MigrationRecord) string {
		if showGroup {
			return fmt.Sprintf("%d %s [%s]", rec.Version, rec.Name, groupLabel(rec.Group))
		}
		return fmt.Sprintf("%d %s", rec.Version, rec.Name)
	}
	for _, rec := range status.Modified {
		cli.Printf("    "+cli.Yellow("⚠")+" %s: MODIFIED since it was applied (up refuses; revert the file, or roll it back and re-apply)\n",
			label(rec))
	}
	for _, rec := range status.Unregistered {
		cli.Printf("    "+cli.Yellow("⚠")+" %s: applied, but no file in migrations/ registers it (down cannot roll it back)\n",
			label(rec))
	}
}

func migrateArgsError(args []string) error {
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		return fmt.Errorf("directory 'migrations/' not found.\n" +
			"This subcommand reads file-based migrations (migrations/*.sql), but apps scaffolded by `gofastr init` register migrations inline in Go (entities/entities.go → RegisterMigrations) and apply them at boot: there may be nothing on disk to read.\n" +
			"Review inline migrations in your entities package; to adopt file-based migrations, generate one with `gofastr migrate generate <name> --from=<gofastr.yml>` (see `gofastr docs migrations`).")
	}
	if getMigrateDBURL(args) == "" {
		return fmt.Errorf("database URL is required; set DATABASE_URL or pass --db-url=<url>")
	}
	driver := getMigrateDriver(args)
	if !hasMigrationDialect(driver) {
		return fmt.Errorf("the migration runner speaks SQLite and Postgres; --db=%q is neither (use sqlite or postgres)", driver)
	}
	return ensureDriverRegistered(driver)
}

func hasMigrationDialect(driver string) bool {
	switch driver {
	case "sqlite", "sqlite3", "postgres", "pgx":
		return true
	}
	return false
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
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	if vals, err := dotenv.Load(framework.DefaultDotEnvPaths()...); err == nil {
		if v, ok := vals["DATABASE_URL"]; ok {
			return v
		}
	}
	return ""
}

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
		if v, ok := migrateDriverFlag(a); ok {
			return project.CanonicalDBDriver(v)
		}
	}
	if isPostgresURL(getMigrateDBURL(args)) {
		return "postgres"
	}
	return "sqlite3"
}

func isPostgresURL(url string) bool {
	scheme, _, ok := strings.Cut(url, "://")
	if !ok {
		return false
	}
	switch strings.ToLower(scheme) {
	case "postgres", "postgresql":
		return true
	}
	return false
}

func migrateDriverFlag(arg string) (string, bool) {
	if v, ok := strings.CutPrefix(arg, "--db="); ok {
		return v, true
	}
	return strings.CutPrefix(arg, "--driver=")
}

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

func groupLabel(g string) string {
	if g == "" {
		return "default"
	}
	return g
}
