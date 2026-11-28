package migratecmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/framework"
)

func runMigrateGenerate(args []string) {
	opts := parseMigrateGenOptions(args)
	if opts.name == "" {
		cli.UsageFail("Usage: gofastr migrate generate <name> --from=<gofastr.yml> [--migrations=dir] [--snapshot=path] [--driver=name]")
	}
	if opts.from == "" {
		cli.UsageFail("An app spec is required: pass --from=<gofastr.yml>.")
	}
	if opts.group != "" {
		if err := migrate.ValidateGroupName(opts.group); err != nil {
			cli.UsageFail("%v", err)
		}
	}

	dialect, err := generateDialect(opts.driver)
	if err != nil {
		cli.UsageFail("%v", err)
	}

	bp, err := appspec.LoadAppSpec(opts.from)
	if err != nil {
		cli.Fail("Failed to load app spec %s: %v", opts.from, err)
		cli.OsExit(1)
	}
	if !opts.driverSet {
		if d := specDriver(bp); d != "" {
			dialect, err = generateDialect(d)
			if err != nil {
				cli.Fail("app spec %s sets app.db.driver %q: %v", opts.from, bp.App.DBDriver, err)
				cli.OsExit(1)
				return
			}
		}
	}
	decls := bp.Entities
	if len(decls) == 0 {
		cli.Fail("App spec %s declares no entities.", opts.from)
		cli.OsExit(1)
	}
	reg := framework.NewRegistry()
	if err := registerSpecEntities(reg, decls); err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
	}

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
		cli.Fail("Generate failed: %v", err)
		cli.OsExit(1)
	}
	if path == "" {
		cli.Success("Schema is up to date: nothing to generate.")
		return
	}

	cli.Success("Generated %s", path)
	if body, err := os.ReadFile(path); err == nil {
		if drops := destructiveDrops(string(body)); len(drops) > 0 {
			cli.Warn("Destructive: `gofastr migrate up` will run %s.", strings.Join(drops, ", "))
			cli.Info("Review data-copy and rename steps: a drop can lose data, and Down restores the shape, not deleted data. Declare a rename (entity renames: in the app spec) instead of dropping and re-adding.")
		}
	}
	cli.Info("Review it, commit it, then run `gofastr migrate up`.")
	if body, err := os.ReadFile(path); err == nil &&
		!strings.Contains(string(body), "-- +migrate Down") {
		cli.Info("Note: this migration has no Down section (no safe inverse): it is not reversible.")
	}
}

func destructiveDrops(content string) []string {
	up, ok := upSection(content)
	if !ok {
		return nil
	}
	var drops []string
	walkSQL(up, func(stmt string) {
		stmt = strings.TrimSpace(stmt)
		if t, ok := dropTableTarget(stmt); ok {
			drops = append(drops, "DROP TABLE "+t)
			return
		}
		if t, c, ok := dropColumnTarget(stmt); ok {
			drops = append(drops, "DROP COLUMN "+t+"."+c)
		}
	}, func(comment string) {
		if m := rebuildDropNote.FindStringSubmatch(comment); m != nil {
			drops = append(drops, "DROP COLUMN "+unquoteIdent(m[1])+"."+unquoteIdent(m[2]))
		}
	})
	return drops
}

func upSection(content string) (string, bool) {
	var b strings.Builder
	in := false
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(line, "-- +migrate") {
			if in {
				break
			}
			in = strings.HasPrefix(line, "-- +migrate Up")
			continue
		}
		if in {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String(), in
}

func walkSQL(sql string, stmt, lineComment func(string)) {
	var cur []byte
	var quote byte
	var escapes bool
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case quote == '\'':
			cur = append(cur, c)
			if escapes && c == '\\' && i+1 < len(sql) {
				i++
				cur = append(cur, sql[i])
			} else if c == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' { // escaped ''
					cur = append(cur, '\'')
					i++
				} else {
					quote = 0
				}
			}
		case quote == '"':
			cur = append(cur, c)
			if c == '"' {
				if i+1 < len(sql) && sql[i+1] == '"' { // escaped ""
					cur = append(cur, '"')
					i++
				} else {
					quote = 0
				}
			}
		case c == '\'' || c == '"':
			quote = c
			escapes = i > 0 && (sql[i-1] == 'E' || sql[i-1] == 'e')
			cur = append(cur, c)
		case c == '$' && dollarQuoteRe.FindString(sql[i:]) != "":
			delimiter := dollarQuoteRe.FindString(sql[i:])
			end := strings.Index(sql[i+len(delimiter):], delimiter)
			if end < 0 {
				cur = append(cur, sql[i:]...)
				i = len(sql)
			} else {
				length := len(delimiter)*2 + end
				cur = append(cur, sql[i:i+length]...)
				i += length - 1
			}
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			start := i
			for ; i < len(sql) && sql[i] != '\n'; i++ {
			}
			lineComment(sql[start:i])
			cur = append(cur, ' ')
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			depth := 1
			i += 2
			for i < len(sql) && depth > 0 {
				switch {
				case strings.HasPrefix(sql[i:], "/*"):
					depth++
					i += 2
				case strings.HasPrefix(sql[i:], "*/"):
					depth--
					i += 2
				default:
					i++
				}
			}
			i--
			cur = append(cur, ' ')
		case c == ';':
			stmt(string(cur))
			cur = cur[:0]
		default:
			cur = append(cur, c)
		}
	}
	if s := strings.TrimSpace(string(cur)); s != "" {
		stmt(string(cur))
	}
}

const (
	identPart = `(?:\"[^\"]+\"|[A-Za-z_][A-Za-z0-9_$]*)`
	qualName  = identPart + `(?:\.` + identPart + `)*`
)

var (
	dollarQuoteRe   = regexp.MustCompile(`^\$(?:[A-Za-z_][A-Za-z0-9_]*)?\$`)
	dropTableRe     = regexp.MustCompile(`(?i)^DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(` + qualName + `)`)
	dropColumnRe    = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+(` + qualName + `)\s+DROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?(` + identPart + `)`)
	rebuildDropNote = regexp.MustCompile(`^-- DROP COLUMN (` + qualName + `)\.(` + identPart + `) by table rebuild`)
)

func dropTableTarget(stmt string) (string, bool) {
	m := dropTableRe.FindStringSubmatch(stmt)
	if m == nil {
		return "", false
	}
	return unquoteIdent(m[1]), true
}

func dropColumnTarget(stmt string) (table, column string, ok bool) {
	m := dropColumnRe.FindStringSubmatch(stmt)
	if m == nil {
		return "", "", false
	}
	return unquoteIdent(m[1]), unquoteIdent(m[2]), true
}

func unquoteIdent(s string) string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(p, `"`)
	}
	return strings.Join(parts, ".")
}

func generateDialect(driver string) (framework.Dialect, error) {
	switch project.CanonicalDBDriver(driver) {
	case "sqlite3":
		return framework.DialectSQLite, nil
	case "postgres", "pgx":
		return framework.DialectPostgres, nil
	}
	return "", fmt.Errorf("migrate generate writes SQLite or Postgres DDL; --db=%q is neither (use sqlite or postgres)", driver)
}

func registerSpecEntities(reg *framework.Registry, decls []framework.EntityDeclaration) error {
	for _, decl := range decls {
		cfg, err := decl.Config()
		if err != nil {
			return fmt.Errorf("entity %q: %w", decl.Name, err)
		}
		if err := reg.Register(framework.Define(decl.Name, cfg)); err != nil {
			return fmt.Errorf("entity %q: %w", decl.Name, err)
		}
	}
	return nil
}

func specDriver(bp appspec.AppSpec) string {
	if d := strings.TrimSpace(bp.App.DBDriver); d != "" {
		return project.CanonicalDBDriver(d)
	}
	if isPostgresURL(strings.TrimSpace(bp.App.DBURL)) {
		return "postgres"
	}
	return ""
}

type migrateGenOptions struct {
	name          string
	from          string
	migrationsDir string
	snapshotPath  string
	driver        string
	driverSet     bool // --db or --driver was passed; it wins over the app spec
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
		case strings.HasPrefix(arg, "--db="), strings.HasPrefix(arg, "--driver="):
			v, _ := migrateDriverFlag(arg)
			opts.driver = project.CanonicalDBDriver(v)
			opts.driverSet = true
		case strings.HasPrefix(arg, "--group="):
			opts.group = strings.TrimPrefix(arg, "--group=")
		case strings.HasPrefix(arg, "--"):
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
