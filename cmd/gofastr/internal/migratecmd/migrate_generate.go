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

// runMigrateGenerate turns a change to the entity declarations into a reviewable,
// reversible, versioned migration file: the offline declarative workflow.
//
//	gofastr migrate generate <name> --from=<gofastr.yml> [--migrations=dir]
//	                                [--snapshot=path] [--driver=name]
//
// It diffs the app spec's entities against a committed schema snapshot (no
// database needed), writes `migrations/NNNN_<name>.sql` with Up and Down
// sections, and updates the snapshot. Review the file, commit it, then
// `gofastr migrate up`.
func runMigrateGenerate(args []string) {
	opts := parseMigrateGenOptions(args)
	if opts.name == "" {
		cli.Fail("Usage: gofastr migrate generate <name> --from=<gofastr.yml> [--migrations=dir] [--snapshot=path] [--driver=name]")
		cli.OsExit(1)
	}
	if opts.from == "" {
		cli.Fail("An app spec is required: pass --from=<gofastr.yml>.")
		cli.OsExit(1)
	}
	if opts.group != "" {
		if err := migrate.ValidateGroupName(opts.group); err != nil {
			cli.Fail("%v", err)
			cli.OsExit(1)
		}
	}

	dialect, err := generateDialect(opts.driver)
	if err != nil {
		cli.Fail("%v", err)
		cli.OsExit(1)
		return
	}

	bp, err := appspec.LoadAppSpec(opts.from)
	if err != nil {
		cli.Fail("Failed to load app spec %s: %v", opts.from, err)
		cli.OsExit(1)
	}
	decls := bp.Entities
	if len(decls) == 0 {
		cli.Fail("App spec %s declares no entities.", opts.from)
		cli.OsExit(1)
	}
	reg := framework.NewRegistry()
	for _, decl := range decls {
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

	// Both generation paths, this CLI and a host binary calling
	// framework.GenerateMigrationFile with its own compiled registry, go
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

// destructiveDrops names every DROP TABLE and DROP COLUMN a rendered
// migration's Up section runs. A drop can lose rows the Down cannot restore,
// and an undeclared rename generates as DROP + CREATE, so the reviewer
// gets one loud line naming each drop before the file is committed. Only
// the Up counts: the Down of an additive migration drops what it added,
// which is the rollback, not a loss. Statements are split with string
// literals, quoted identifiers and comments respected, so a DEFAULT
// carrying the words DROP TABLE is not a drop.
//
// A SQLite rebuild drops a column with no DROP COLUMN statement: the new
// table leaves it out. The generator writes one comment line above the
// rebuild for each such column (rebuildDropNote), and that line, the one
// comment read here, is named like the statement it stands in for.
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

// upSection returns the body between the "-- +migrate Up" directive and
// the next directive (or EOF), the statements `migrate up` runs.
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

// walkSQL splits SQL on ";" outside single-quoted strings, double-quoted
// identifiers, dollar-quoted bodies and comments, and hands each statement
// to stmt in order. Each "--" comment outside those goes to lineComment,
// whole, as it is met.
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

// identPart and qualName match the spellings query.QuoteKeyword emits:
// a part is bare unless it is a keyword, and a schema-qualified table
// (`archive.posts`, `posts."order"`) is parts joined by dots.
const (
	identPart = `(?:\"[^\"]+\"|[A-Za-z_][A-Za-z0-9_$]*)`
	qualName  = identPart + `(?:\.` + identPart + `)*`
)

var (
	dollarQuoteRe = regexp.MustCompile(`^\$(?:[A-Za-z_][A-Za-z0-9_]*)?\$`)
	dropTableRe   = regexp.MustCompile(`(?i)^DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?(` + qualName + `)`)
	dropColumnRe  = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+(` + qualName + `)\s+DROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?(` + identPart + `)`)
	// rebuildDropNote matches the comment framework/migrate writes above a
	// SQLite rebuild for each column the rebuild leaves out
	// ("-- DROP COLUMN posts.slug by table rebuild: …"). The table is
	// greedy and the column one part, so a schema-qualified table keeps
	// its dots.
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

// unquoteIdent strips each part's keyword quotes, keeping the dots of a
// schema-qualified name.
func unquoteIdent(s string) string {
	parts := strings.Split(s, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(p, `"`)
	}
	return strings.Join(parts, ".")
}

// generateDialect is the DDL dialect for a --db/--driver value. The
// generator writes SQLite and Postgres only; anything else (mysql, or a
// typo) fell back to SQLite in silence and committed DDL for an engine
// nobody named.
func generateDialect(driver string) (framework.Dialect, error) {
	switch project.CanonicalDBDriver(driver) {
	case "sqlite3":
		return framework.DialectSQLite, nil
	case "postgres", "pgx":
		return framework.DialectPostgres, nil
	}
	return "", fmt.Errorf("migrate generate writes SQLite or Postgres DDL; --db=%q is neither (use sqlite or postgres)", driver)
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
		case strings.HasPrefix(arg, "--db="), strings.HasPrefix(arg, "--driver="):
			v, _ := migrateDriverFlag(arg)
			opts.driver = project.CanonicalDBDriver(v)
		case strings.HasPrefix(arg, "--group="):
			opts.group = strings.TrimPrefix(arg, "--group=")
		case strings.HasPrefix(arg, "--"):
			// unknown flag: ignore
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
