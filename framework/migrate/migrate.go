package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

var jsonNumberLiteral = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// Dialect identifies the SQL dialect the migrator emits for.
type Dialect = coremig.Dialect

// The dialects the migrator supports.
const (
	DialectSQLite   = coremig.DialectSQLite
	DialectPostgres = coremig.DialectPostgres
)

// DetectDialect returns the dialect of an open *sql.DB, Postgres when
// SELECT version() names it and SQLite otherwise. When the probe cannot
// reach the database it logs and returns SQLite; a caller that decides
// anything beyond DDL types uses DetectDialectStrict.
func DetectDialect(db *sql.DB) Dialect {
	d, err := detectDialectFailClosed(db)
	if err != nil {
		slog.Default().Warn("migrate: dialect probe failed; assuming SQLite for best-effort caller",
			"err", err)
		return DialectSQLite
	}
	return d
}

// DetectDialectStrict is DetectDialect that returns an error, never a
// guess, when the probe keeps failing for a transient reason.
func DetectDialectStrict(db *sql.DB) (Dialect, error) {
	return detectDialectFailClosed(db)
}

func detectDialectFailClosed(db *sql.DB) (Dialect, error) {
	const attempts = 3
	var lastErr error
	for i := range attempts {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var v string
		err := db.QueryRowContext(ctx, "SELECT version()").Scan(&v)
		cancel()
		if err == nil {
			if strings.Contains(strings.ToLower(v), "postgresql") {
				return DialectPostgres, nil
			}
			return DialectSQLite, nil
		}
		lastErr = err
		if !isTransientProbeErr(err) {
			return DialectSQLite, nil
		}
		if i < attempts-1 {
			time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
		}
	}
	return "", fmt.Errorf("dialect probe failed after %d attempts: %w "+
		"(check the database connection / DATABASE_URL)", attempts, lastErr)
}

func isConnectionErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, frag := range []string{
		"unable to open database file",
		"connection refused",
		"connection reset",
		"no such host",
		"dial tcp",
		"i/o timeout",
		"broken pipe",
		"admin shutdown",
		"server closed the connection",
	} {
		if strings.Contains(msg, frag) {
			return true
		}
	}
	return false
}

func wrapConnErr(err error) error {
	if err == nil || !isConnectionErr(err) {
		return err
	}
	return fmt.Errorf("%w (cannot reach the database: check the database connection / DATABASE_URL)", err)
}

func isTransientProbeErr(err error) bool {
	msg := stripQuoted(strings.ToLower(err.Error()))
	for _, deterministic := range []string{"no such function", "unknown function", "syntax error"} {
		if strings.Contains(msg, deterministic) {
			return false
		}
	}
	return true
}

func stripQuoted(msg string) string {
	var b strings.Builder
	inQuote := false
	for _, r := range msg {
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote {
			b.WriteRune(r)
		}
	}
	return b.String()
}

type execQueryer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// AutoMigrate is AutoMigrateContext with a background context.
func AutoMigrate(db *sql.DB, registry entity.Registry) error {
	return AutoMigrateContext(context.Background(), db, registry)
}

// AutoMigrateContext converges the database with the registered entities
// additively: it creates missing tables and indexes and adds missing
// columns, and never drops, renames or retypes. The run holds the migration
// lock and is one transaction, so a failure leaves no partial schema.
// db == nil is a no-op.
func AutoMigrateContext(ctx context.Context, db *sql.DB, registry entity.Registry) error {
	return AutoMigratePlanContext(ctx, db, Plan{Registry: registry})
}

// AutoMigratePlanContext is AutoMigrateContext for a full Plan: tables,
// then views, then every routine whose Dialect is empty or matches the
// database, in the same transaction. Each matching routine's Up runs on
// every boot, and the gofastr_routines ledger records its checksum. A
// ledger row with no registered routine is logged, never dropped.
func AutoMigratePlanContext(ctx context.Context, db *sql.DB, plan Plan) error {
	if db == nil {
		return nil
	}
	var ordered []*entity.Entity
	all := map[string]*entity.Entity{}
	if plan.Registry != nil {
		all = UnionEntities(plan.Registry)
		var err error
		ordered, err = topoSortEntities(all)
		if err != nil {
			return err
		}
		if err := checkIndexNames(ordered, all); err != nil {
			return err
		}
	}
	dialect, err := detectDialectFailClosed(db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := checkGeneratedPivotDeclarations(ordered, all, dialect); err != nil {
		return err
	}
	appliedRoutines, skippedRoutines := partitionRoutinesByDialect(plan.Routines, dialect)
	if len(skippedRoutines) > 0 {
		names := make([]string, 0, len(skippedRoutines))
		for _, r := range skippedRoutines {
			names = append(names, r.Name)
		}
		sort.Strings(names)
		slog.Info("migrate: skipping routines declared for a different dialect",
			"declared", dialectString(skippedRoutines[0].Dialect),
			"running", dialectString(dialect),
			"routines", strings.Join(names, ","),
			"count", len(names),
		)
	}
	liveByTable := map[string]map[string]string{}
	tableNames := make([]string, 0, len(ordered))
	for _, ent := range ordered {
		if ent.Config.Unmanaged || len(ent.GetFields()) == 0 {
			continue
		}
		tableNames = append(tableNames, ent.GetTable())
	}
	keysByTable := map[string]liveKey{}
	if len(tableNames) > 0 {
		var err error
		liveByTable, err = ReadLiveColumnsBulk(ctx, db, tableNames, dialect)
		if err != nil {
			return fmt.Errorf("migrate: read live schema: %w", wrapConnErr(err))
		}
		keysByTable, err = readLiveKeysQ(ctx, db, existingEntityTables(ordered, liveByTable), dialect)
		if err != nil {
			return fmt.Errorf("migrate: read live schema: %w", wrapConnErr(err))
		}
	}

	if err := coremig.WithAdvisoryLock(ctx, db, dialect, func(conn *sql.Conn) error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("migrate: begin tx: %w", err)
		}
		defer func() { _ = tx.Rollback() }()
		var deferred []SchemaChange
		for _, ent := range ordered {
			var lk *liveKey
			if len(liveByTable[ent.GetTable()]) > 0 {
				k := keysByTable[ent.GetTable()]
				lk = &k
			}
			if err := migrateEntity(ctx, tx, ent, all, dialect, liveByTable[ent.GetTable()], lk, &deferred); err != nil {
				return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
			}
		}
		for _, c := range deferred {
			if _, err := tx.ExecContext(ctx, c.SQL); err != nil {
				return fmt.Errorf("migrate: %s: %w", c.Summary, err)
			}
		}
		if err := migratePivotTables(ctx, tx, ordered, all, dialect); err != nil {
			return err
		}
		if len(plan.Routines) > 0 {
			if err := ensureRoutineLedger(ctx, tx, dialect); err != nil {
				return fmt.Errorf("migrate: ensure routine ledger: %w", err)
			}
			prevLedger, err := readRoutineLedger(ctx, tx)
			if err != nil {
				return fmt.Errorf("migrate: read routine ledger: %w", err)
			}

			if err := applyViews(ctx, tx, plan.Views, dialect); err != nil {
				return err
			}
			for _, r := range appliedRoutines {
				if _, err := tx.ExecContext(ctx, r.Up); err != nil {
					return fmt.Errorf("migrate routine %s: %w", r.Name, err)
				}
			}

			var (
				appliedCount   = len(appliedRoutines)
				changedCount   int
				firstTimeCount int
			)
			for _, r := range appliedRoutines {
				newSum := RoutineChecksum(r)
				if oldSum, ok := prevLedger[r.Name]; ok {
					if oldSum != newSum {
						changedCount++
					}
				} else {
					firstTimeCount++
				}
				if err := upsertRoutineLedger(ctx, tx, dialect, r.Name, newSum); err != nil {
					return fmt.Errorf("migrate: record routine %s: %w", r.Name, err)
				}
			}

			registeredNames := make(map[string]struct{}, len(appliedRoutines)+len(skippedRoutines))
			for _, r := range appliedRoutines {
				registeredNames[r.Name] = struct{}{}
			}
			for _, r := range skippedRoutines {
				registeredNames[r.Name] = struct{}{}
			}
			var orphaned []string
			for name := range prevLedger {
				if _, ok := registeredNames[name]; !ok {
					orphaned = append(orphaned, name)
				}
			}
			sort.Strings(orphaned)
			for _, name := range orphaned {
				slog.Warn("migrate: previously applied routine is no longer registered; not dropped (additive-only)",
					"routine", name,
					"hint", "drop via Routine.Down in a versioned migration, or remove the ledger row manually",
				)
			}

			slog.Info("migrate: routine apply summary",
				"applied", appliedCount,
				"changed", changedCount,
				"first_time", firstTimeCount,
				"skipped", len(skippedRoutines),
				"orphaned", len(orphaned),
			)
		} else {
			if err := applyViews(ctx, tx, plan.Views, dialect); err != nil {
				return err
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrate: commit: %w", err)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("migrate: %w", wrapConnErr(err))
	}
	if dialect == DialectSQLite {
		stale, scanErr := FindStaleOwnerForeignKeys(ctx, db, plan.Registry)
		if scanErr != nil {
			slog.Warn("migrate: could not scan for stale owner-column foreign keys", "err", scanErr)
		}
		for _, s := range stale {
			slog.Warn("migrate: table carries a foreign key on the column its entity declares as Scope.OwnerField; creates that stamp it from the session fail unless the referenced table holds those ids",
				"table", s.Table,
				"column", s.Column,
				"references", s.References,
				"fix", "gofastr migrate repair --from=<gofastr.yml> --db-url=<url> --apply",
			)
		}
	}
	return nil
}

func partitionRoutinesByDialect(routines []Routine, running Dialect) (applies, skipped []Routine) {
	applies = make([]Routine, 0, len(routines))
	skipped = make([]Routine, 0)
	for _, r := range routines {
		if r.Dialect == "" || r.Dialect == running {
			applies = append(applies, r)
			continue
		}
		skipped = append(skipped, r)
	}
	return applies, skipped
}

// MigrateEntity creates one entity's table and indexes if the table does
// not exist, with no foreign keys and no column convergence (AutoMigrate
// does both). The dialect is detected from db.
func MigrateEntity(db *sql.DB, ent *entity.Entity) error {
	dialect, err := detectDialectFailClosed(db)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return migrateEntity(context.Background(), db, ent, nil, dialect, nil, nil, nil)
}

// MigrateEntityDialect is MigrateEntity for a known dialect.
func MigrateEntityDialect(db *sql.DB, ent *entity.Entity, dialect Dialect) error {
	return migrateEntity(context.Background(), db, ent, nil, dialect, nil, nil, nil)
}

func migrateEntity(ctx context.Context, exec execQueryer, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string, lk *liveKey, deferred *[]SchemaChange) error {
	if ent.Config.Unmanaged {
		return nil
	}
	if len(ent.GetFields()) == 0 {
		return nil
	}

	if len(live) == 0 && all != nil {
		table := ent.GetTable()
		fresh, err := readEntityColumnsQ(ctx, exec, table, dialect)
		if err != nil {
			return fmt.Errorf("re-read columns for %s: %w", table, err)
		}
		if len(fresh) > 0 {
			keys, err := readLiveKeysQ(ctx, exec, []string{table}, dialect)
			if err != nil {
				return fmt.Errorf("re-read the key of %s: %w", table, err)
			}
			k := keys[table]
			live, lk = fresh, &k
		}
	}

	if len(live) == 0 {
		stmt, err := buildCreateTableSQL(ent, all, dialect)
		if err != nil {
			return fmt.Errorf("migrate %s: %w", ent.GetName(), err)
		}
		if _, err := exec.ExecContext(ctx, stmt); err != nil {
			return err
		}
	} else {
		if err := addMissingColumns(ctx, exec, ent, all, dialect, live); err != nil {
			return err
		}
		if lk != nil {
			if err := convergeKey(ctx, exec, ent, all, dialect, *lk, deferred); err != nil {
				return err
			}
		}
		if err := ensureUniqueIndexes(ctx, exec, ent, dialect); err != nil {
			return err
		}
		if err := refuseStaleUniques(ctx, exec, ent, dialect); err != nil {
			return err
		}
	}

	safeTable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return fmt.Errorf("migrate %s: invalid table name %q: %w", ent.GetName(), ent.GetTable(), err)
	}

	for _, idx := range entityIndices(ent) {
		if _, err := exec.ExecContext(ctx, indexDDL(dialect, safeTable, idx)); err != nil {
			return fmt.Errorf("create index on %s: %w", ent.GetTable(), err)
		}
	}
	return nil
}

func addMissingColumns(ctx context.Context, exec execQueryer, ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, preRead map[string]string) error {
	adds, err := additiveChanges(ent, all, dialect, preRead)
	if err != nil || len(adds) == 0 {
		return err
	}
	liveNow, err := readEntityColumnsQ(ctx, exec, ent.GetTable(), dialect)
	if err != nil {
		return fmt.Errorf("re-read columns for %s: %w", ent.GetTable(), err)
	}
	if len(liveNow) == 0 {
		return nil
	}
	adds, err = additiveChanges(ent, all, dialect, liveNow)
	if err != nil {
		return err
	}
	for _, c := range adds {
		if _, err := exec.ExecContext(ctx, c.SQL); err != nil {
			return fmt.Errorf("%s: %w", c.Summary, err)
		}
	}
	return nil
}

func additiveChanges(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect, live map[string]string) ([]SchemaChange, error) {
	changes, err := diffEntityFromLive(ent, all, dialect, live)
	if err != nil {
		return nil, err
	}
	adds := changes[:0]
	for _, c := range changes {
		if c.Destructive || isRenameChange(c.SQL) {
			continue
		}
		adds = append(adds, c)
	}
	return adds, nil
}

func isRenameChange(sql string) bool {
	return strings.Contains(strings.ToUpper(sql), " RENAME COLUMN ")
}

func indexDDL(dialect Dialect, table string, idx entity.Index) string {
	name := idx.Name
	if name == "" {
		if idx.Expression != "" {
			panic(fmt.Sprintf("migrate: index on %s has Expression but no Name: expression indices require an explicit Name", table))
		}
		name = defaultIndexName(table, idx.Columns)
	}
	safeName, err := query.SafeIdent(name)
	if err != nil {
		panic(fmt.Sprintf("migrate: invalid index name %q: %v", name, err))
	}
	var body string
	if idx.Expression != "" {
		body = sanitizeIndexExpression(idx.Expression)
	} else {
		safeCols := make([]string, len(idx.Columns))
		for i, col := range idx.Columns {
			safeCols[i] = ddlIdent(query.MustIdent(col))
		}
		body = strings.Join(safeCols, ", ")
	}
	return createIndexSQL(dialect, idx.Unique, safeName, table, body)
}

func sanitizeIndexExpression(expr string) string {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		panic("migrate: index Expression is empty after trim")
	}
	for _, banned := range []string{";", "--", "/*", "*/"} {
		if strings.Contains(trimmed, banned) {
			panic(fmt.Sprintf("migrate: index Expression %q contains forbidden token %q", trimmed, banned))
		}
	}
	return trimmed
}

func foreignKeyClauses(ent *entity.Entity, all map[string]*entity.Entity, dialect Dialect) ([]string, error) {
	fks, err := declaredForeignKeys(ent, all)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(fks))
	for i, fk := range fks {
		out[i] = fk.clause(dialect)
	}
	return out, nil
}

type declaredFK struct {
	cols     []string
	target   *entity.Entity
	refTable string
	refCols  []string
	onDelete entity.OnDeleteAction
}

func (fk declaredFK) clause(dialect Dialect) string {
	c := fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s(%s)",
		strings.Join(ddlIdents(fk.cols), ", "), referencedTable(dialect, fk.refTable), strings.Join(ddlIdents(fk.refCols), ", "))
	if fk.onDelete != "" {
		c += " ON DELETE " + string(fk.onDelete)
	}
	return c
}

func declaredForeignKeys(ent *entity.Entity, all map[string]*entity.Entity) ([]declaredFK, error) {
	var out []declaredFK
	seen := make(map[string]bool)
	for _, rel := range ent.Config.Relations {
		fks := rel.ForeignKey
		if rel.Type != entity.RelManyToOne || len(fks) == 0 {
			continue
		}
		dedup := strings.Join(fks, ",")
		if seen[dedup] {
			continue
		}
		if len(fks) == 1 && ent.Config.Scope.OwnerField != "" && strings.EqualFold(fks[0], ent.Config.Scope.OwnerField) {
			seen[dedup] = true
			continue
		}
		seen[dedup] = true
		target, ok := all[rel.Entity]
		if !ok {
			return nil, fmt.Errorf("relation %q references unknown entity %q", rel.Name, rel.Entity)
		}
		if err := entity.CheckRelationKeys(ent, rel, target); err != nil {
			return nil, fmt.Errorf("relation %q: %w", rel.Name, err)
		}
		safeFKs, err := query.SafeColumns(fks, "column")
		if err != nil {
			return nil, fmt.Errorf("relation %q: invalid FK: %w", rel.Name, err)
		}
		safeTargetTable, err := query.SafeIdent(target.GetTable())
		if err != nil {
			return nil, fmt.Errorf("relation %q: invalid target table %q: %w", rel.Name, target.GetTable(), err)
		}
		safeTargetKey, err := query.SafeColumns(target.KeyColumns(), "column")
		if err != nil {
			return nil, fmt.Errorf("relation %q: invalid target key: %w", rel.Name, err)
		}
		switch rel.OnDelete {
		case "", entity.OnDeleteCascade, entity.OnDeleteSetNull, entity.OnDeleteRestrict, entity.OnDeleteNoAction:
		default:
			return nil, fmt.Errorf("relation %q: invalid ON DELETE action %q", rel.Name, rel.OnDelete)
		}
		out = append(out, declaredFK{
			cols:     safeFKs,
			target:   target,
			refTable: safeTargetTable,
			refCols:  safeTargetKey,
			onDelete: rel.OnDelete,
		})
	}
	return out, nil
}

func checkForeignKeyTypes(ent *entity.Entity, rel entity.Relation, all map[string]*entity.Entity) error {
	fks := rel.ForeignKey
	if len(fks) < 2 {
		return nil
	}
	holder, ref := ent, all[rel.Entity]
	if rel.Type == entity.RelHasOne || rel.Type == entity.RelHasMany {
		holder, ref = ref, ent
	}
	if holder == nil || ref == nil {
		return nil
	}
	for i, k := range ref.KeyColumns() {
		ft := keyColumnType(holder, fks[i], all, DialectPostgres, map[columnRef]bool{})
		kt := keyColumnType(ref, k, all, DialectPostgres, map[columnRef]bool{})
		if typeFamily(ft) != typeFamily(kt) {
			return fmt.Errorf("%w: %s.%s is %s and references %s.%s, which is %s",
				entity.ErrForeignKeyShape, holder.GetName(), fks[i], ft, ref.GetName(), k, kt)
		}
	}
	return nil
}

func typeFamily(t string) string {
	t = strings.ToUpper(t)
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = t[:i]
	}
	switch t {
	case "SMALLINT", "INTEGER", "INT", "BIGINT":
		return "INTEGER"
	case "TEXT", "VARCHAR", "CHAR", "CHARACTER VARYING":
		return "TEXT"
	}
	return t
}

func mutatingFKReferrers(ent *entity.Entity, all map[string]*entity.Entity) []string {
	if ent == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, name := range slices.Sorted(maps.Keys(all)) {
		other := all[name]
		for _, rel := range other.Config.Relations {
			switch rel.Type {
			case entity.RelManyToMany:
				if rel.Through != "" && (other == ent || all[rel.Entity] == ent) {
					seen[rel.Through] = true
				}
			case entity.RelManyToOne:
				if all[rel.Entity] != ent || len(rel.ForeignKey) == 0 {
					continue
				}
				if other.Config.Scope.OwnerField != "" && len(rel.ForeignKey) == 1 && strings.EqualFold(rel.ForeignKey[0], other.Config.Scope.OwnerField) {
					continue
				}
				if rel.OnDelete == entity.OnDeleteCascade || rel.OnDelete == entity.OnDeleteSetNull {
					seen[other.GetTable()] = true
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// UnionEntities returns one entity per name. A name with one registered
// version maps to that entity unchanged; a name with several maps to a copy
// of its unversioned (else lex-first) version whose fields, indexes,
// relations and seed are the union of every version's, first declaration
// winning.
func UnionEntities(reg entity.Registry) map[string]*entity.Entity {
	sorted := reg.AllSorted()
	byName := make(map[string][]*entity.Entity, len(sorted))
	for _, e := range sorted {
		byName[e.Config.Name] = append(byName[e.Config.Name], e)
	}
	out := make(map[string]*entity.Entity, len(byName))
	for name, versions := range byName {
		if len(versions) == 1 {
			out[name] = versions[0]
			continue
		}
		out[name] = mergeVersions(versions)
	}
	return out
}

func mergeVersions(versions []*entity.Entity) *entity.Entity {
	base := versions[0]
	for _, e := range versions {
		if e.Version == "" {
			base = e
			break
		}
	}
	merged := *base
	merged.Config = mergeEntityConfig(base.Config, versions)
	return &merged
}

func mergeEntityConfig(base entity.EntityConfig, versions []*entity.Entity) entity.EntityConfig {
	cfg := base
	seenField := make(map[string]bool)
	fields := make([]schema.Field, 0, len(base.Fields))
	for _, v := range versions {
		for _, f := range v.Config.Fields {
			key := strings.ToLower(f.Name)
			if seenField[key] {
				continue
			}
			seenField[key] = true
			fields = append(fields, f)
		}
	}
	cfg.Fields = fields

	seenIdx := make(map[string]bool)
	var indices []entity.Index
	for _, v := range versions {
		for _, idx := range v.Config.Indices {
			name := idx.Name
			if name == "" && len(idx.Columns) > 0 {
				name = defaultIndexName(v.Config.Table, idx.Columns)
			}
			if name != "" && seenIdx[name] {
				continue
			}
			if name != "" {
				seenIdx[name] = true
			}
			indices = append(indices, idx)
		}
	}
	cfg.Indices = indices

	seenRel := make(map[string]bool)
	var rels []entity.Relation
	for _, v := range versions {
		for _, rel := range v.Config.Relations {
			key := strings.Join(rel.ForeignKey, ",")
			if key == "" {
				key = rel.Name
			}
			if seenRel[key] {
				continue
			}
			seenRel[key] = true
			rels = append(rels, rel)
		}
	}
	cfg.Relations = rels

	for _, v := range versions {
		if v.Config.Seed != nil {
			cfg.Seed = v.Config.Seed
			cfg.SeedFS = v.Config.SeedFS
			cfg.SeedPath = v.Config.SeedPath
		}
	}

	return cfg
}

func topoSortEntities(all map[string]*entity.Entity) ([]*entity.Entity, error) {
	names := make([]string, 0, len(all))
	for n := range all {
		names = append(names, n)
	}
	sort.Strings(names)

	visited := make(map[string]bool)
	tempMark := make(map[string]bool)
	out := make([]*entity.Entity, 0, len(all))

	var visit func(name string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if tempMark[name] {
			return nil
		}
		ent := all[name]
		tempMark[name] = true
		for _, rel := range ent.Config.Relations {
			if err := entity.CheckRelationKeys(ent, rel, all[rel.Entity]); err != nil {
				return fmt.Errorf("entity %q relation %q: %w", name, rel.Name, err)
			}
			if err := checkForeignKeyTypes(ent, rel, all); err != nil {
				return fmt.Errorf("entity %q relation %q: %w", name, rel.Name, err)
			}
			if rel.Type == entity.RelManyToMany {
				if err := checkPivotEntity(ent, rel, all); err != nil {
					return err
				}
			}
			if rel.Type == entity.RelManyToOne {
				if _, ok := all[rel.Entity]; !ok {
					return fmt.Errorf("entity %q has BelongsTo to unknown entity %q", name, rel.Entity)
				}
				if err := visit(rel.Entity); err != nil {
					return err
				}
			}
		}
		tempMark[name] = false
		visited[name] = true
		out = append(out, ent)
		return nil
	}

	for _, n := range names {
		if err := visit(n); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SQLType maps a field to its SQL column type for dialect; a RawType is
// returned as written. An Int is BIGINT on Postgres (BIGSERIAL when
// auto-incremented) and INTEGER on SQLite.
func SQLType(f schema.Field, dialect Dialect) string {
	if f.RawType != "" {
		return f.RawType
	}
	switch f.Type {
	case schema.String:
		if n, ok := schema.VarcharWidth(f.Max); ok {
			return fmt.Sprintf("VARCHAR(%d)", n)
		}
		return "TEXT"
	case schema.Text:
		return "TEXT"
	case schema.Int:
		if dialect == DialectPostgres {
			if f.AutoGenerate == schema.AutoIncrement {
				return "BIGSERIAL"
			}
			return "BIGINT"
		}
		return "INTEGER"
	case schema.Float:
		if dialect == DialectPostgres {
			return "DOUBLE PRECISION"
		}
		return "REAL"
	case schema.Bool:
		return "BOOLEAN"
	case schema.Decimal:
		return schema.DecimalColumnType
	case schema.Enum:
		return "TEXT"
	case schema.UUID:
		return "TEXT"
	case schema.Timestamp:
		if dialect == DialectPostgres {
			return "TIMESTAMPTZ"
		}
		return "DATETIME"
	case schema.Date:
		return "DATE"
	case schema.JSON:
		if dialect == DialectPostgres {
			return "JSONB"
		}
		return "TEXT"
	case schema.Relation:
		return "TEXT"
	case schema.Image:
		return "TEXT"
	case schema.File:
		return "TEXT"
	default:
		return "TEXT"
	}
}

// ColumnDefaultClause returns a column's " DEFAULT ..." fragment, with its
// leading space, or "". An explicit Default wins; otherwise an AutoUUID
// field on Postgres defaults to gen_random_uuid(). An AutoTimestamp field
// gets no default: the application stamps it.
func ColumnDefaultClause(f schema.Field, dialect Dialect) string {
	if f.Default != nil {
		return " DEFAULT " + SQLDefault(f, dialect)
	}
	if f.AutoGenerate == schema.AutoUUID && dialect == DialectPostgres {
		return " DEFAULT gen_random_uuid()"
	}
	return ""
}

// SQLDefault renders a field's Default as a SQL literal: integers and
// floats bare, a bool as TRUE/FALSE on Postgres and 1/0 on SQLite, a JSON
// field's non-string value as its JSON text, and anything else as a quoted
// string literal that cannot be closed from inside.
func SQLDefault(f schema.Field, dialect Dialect) string {
	if _, isString := f.Default.(string); f.Type == schema.JSON && !isString {
		if b, err := json.Marshal(f.Default); err == nil {
			return quoteSQLLiteral(string(b))
		}
	}
	switch v := f.Default.(type) {
	case string:
		return quoteSQLLiteral(v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		if jsonNumberLiteral.MatchString(string(v)) {
			return string(v)
		}
		return quoteSQLLiteral(string(v))
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		if dialect == DialectPostgres {
			if v {
				return "TRUE"
			}
			return "FALSE"
		}
		if v {
			return "1"
		}
		return "0"
	default:
		return quoteSQLLiteral(fmt.Sprintf("%v", v))
	}
}

func quoteSQLLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func fkColumnSQLType(ent *entity.Entity, pkName string, all map[string]*entity.Entity, dialect Dialect) string {
	return keyColumnType(ent, pkName, all, dialect, map[columnRef]bool{})
}

func keyColumnType(ent *entity.Entity, pkName string, all map[string]*entity.Entity, dialect Dialect, seen map[columnRef]bool) string {
	for _, f := range ent.GetFields() {
		if strings.EqualFold(f.Name, pkName) {
			t := relationColumnType(ent, f, all, dialect, seen)
			if dialect == DialectPostgres {
				switch strings.ToLower(strings.TrimSpace(t)) {
				case "serial", "bigserial", "smallserial":
					return integerFKType(t, dialect)
				}
			}
			return t
		}
	}
	return "TEXT"
}

func columnSQLType(ent *entity.Entity, f schema.Field, all map[string]*entity.Entity, dialect Dialect) string {
	return relationColumnType(ent, f, all, dialect, map[columnRef]bool{})
}

type columnRef struct {
	e   *entity.Entity
	col string
}

func relationColumnType(ent *entity.Entity, f schema.Field, all map[string]*entity.Entity, dialect Dialect, seen map[columnRef]bool) string {
	t := SQLType(f, dialect)
	ref := columnRef{ent, f.Name}
	if f.Type != schema.Relation || f.RawType != "" || all == nil || seen[ref] {
		return t
	}
	seen[ref] = true
	for _, rel := range ent.Config.Relations {
		if rel.Type != entity.RelManyToOne || len(rel.ForeignKey) != 1 || !strings.EqualFold(rel.ForeignKey[0], f.Name) {
			continue
		}
		target, ok := all[rel.Entity]
		if !ok {
			return t
		}
		pk, err := target.KeyColumn()
		if err != nil {
			return t
		}
		if ft := keyColumnType(target, pk, all, dialect, seen); isIntegerType(ft) {
			return integerFKType(ft, dialect)
		}
		return t
	}
	return t
}

func integerFKType(keyType string, dialect Dialect) string {
	if dialect != DialectPostgres {
		return "INTEGER"
	}
	switch canonicalType(keyType, DialectPostgres) {
	case "bigint", "int8":
		return "BIGINT"
	case "smallint", "int2":
		return "SMALLINT"
	}
	return "INTEGER"
}

func generatedPivots(ordered []*entity.Entity, all map[string]*entity.Entity) iter.Seq2[*entity.Entity, entity.Relation] {
	return func(yield func(*entity.Entity, entity.Relation) bool) {
		seen := make(map[string]bool)
		for _, ent := range ordered {
			for _, rel := range ent.Config.Relations {
				if rel.Type != entity.RelManyToMany || rel.Through == "" {
					continue
				}
				key := strings.ToLower(rel.Through)
				if seen[key] {
					continue
				}
				seen[key] = true
				if pivotHasEntity(rel.Through, all) {
					continue
				}
				if !yield(ent, rel) {
					return
				}
			}
		}
	}
}

func checkPivotEntity(ent *entity.Entity, rel entity.Relation, all map[string]*entity.Entity) error {
	for _, name := range slices.Sorted(maps.Keys(all)) {
		e := all[name]
		if !strings.EqualFold(e.GetTable(), rel.Through) {
			if entity.TableSchemaSpellingDiffers(e.GetTable(), rel.Through) {
				return fmt.Errorf("entity %q relation %q: through table %q and join entity %q's table %q may name one table with and without its schema: spell them the same", ent.GetName(), rel.Name, rel.Through, e.GetName(), e.GetTable())
			}
			continue
		}
		for _, col := range []string{rel.LocalKey, rel.ForeignKeyTarget} {
			if !slices.ContainsFunc(e.GetFields(), func(f schema.Field) bool { return strings.EqualFold(f.Name, col) }) {
				return fmt.Errorf("entity %q relation %q: join entity %q declares no column %q", ent.GetName(), rel.Name, e.GetName(), col)
			}
		}
	}
	return nil
}

func pivotHasEntity(table string, all map[string]*entity.Entity) bool {
	if all[table] != nil {
		return true
	}
	for _, e := range all {
		if strings.EqualFold(e.GetTable(), table) {
			return true
		}
	}
	return false
}

type pivotSpec struct {
	Safe         string
	Cols         map[string]string
	DDL          string
	DropDDL      string
	IndexDDL     string
	DropIndexDDL string
	fks          []declaredFK
}

func buildPivotSpec(ent *entity.Entity, rel entity.Relation, all map[string]*entity.Entity, dialect Dialect) (pivotSpec, error) {
	safeThrough, err := query.SafeIdent(rel.Through)
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: invalid through table %q: %w", rel.Name, rel.Through, err)
	}
	target, ok := all[rel.Entity]
	if !ok {
		return pivotSpec{}, fmt.Errorf("relation %q references unknown entity %q", rel.Name, rel.Entity)
	}
	safeLocalKey, err := query.SafeIdent(rel.LocalKey)
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: invalid local key %q: %w", rel.Name, rel.LocalKey, err)
	}
	safeTargetKey, err := query.SafeIdent(rel.ForeignKeyTarget)
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: invalid foreign key target %q: %w", rel.Name, rel.ForeignKeyTarget, err)
	}
	safeSourceTable, err := query.SafeIdent(ent.GetTable())
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: invalid source table %q: %w", rel.Name, ent.GetTable(), err)
	}
	safeTargetTable, err := query.SafeIdent(target.GetTable())
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: invalid target table %q: %w", rel.Name, target.GetTable(), err)
	}
	sourcePK, err := ent.KeyColumn()
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: %w", rel.Name, err)
	}
	safeSourcePK, err := query.SafeIdent(sourcePK)
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: invalid source PK %q: %w", rel.Name, sourcePK, err)
	}
	targetPK, err := target.KeyColumn()
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: %w", rel.Name, err)
	}
	safeTargetPK, err := query.SafeIdent(targetPK)
	if err != nil {
		return pivotSpec{}, fmt.Errorf("relation %q: invalid target PK %q: %w", rel.Name, targetPK, err)
	}

	sourcePKType := fkColumnSQLType(ent, sourcePK, all, dialect)
	targetPKType := fkColumnSQLType(target, targetPK, all, dialect)
	idxName := defaultIndexName(safeThrough, []string{safeTargetKey})
	return pivotSpec{
		Safe: safeThrough,
		Cols: map[string]string{safeLocalKey: sourcePKType, safeTargetKey: targetPKType},
		DDL: fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s (\n\t%s %s NOT NULL,\n\t%s %s NOT NULL,\n\tPRIMARY KEY (%s, %s),\n\tFOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE CASCADE,\n\tFOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE CASCADE\n)",
			ddlIdent(safeThrough),
			ddlIdent(safeLocalKey), sourcePKType,
			ddlIdent(safeTargetKey), targetPKType,
			ddlIdent(safeLocalKey), ddlIdent(safeTargetKey),
			ddlIdent(safeLocalKey), referencedTable(dialect, safeSourceTable), ddlIdent(safeSourcePK),
			ddlIdent(safeTargetKey), referencedTable(dialect, safeTargetTable), ddlIdent(safeTargetPK),
		),
		DropDDL:      fmt.Sprintf("DROP TABLE IF EXISTS %s", ddlIdent(safeThrough)),
		IndexDDL:     createIndexSQL(dialect, false, idxName, safeThrough, ddlIdent(safeTargetKey)),
		DropIndexDDL: dropIndexSQL(safeThrough, idxName),
		fks: []declaredFK{
			{cols: []string{safeLocalKey}, target: ent, refTable: safeSourceTable, refCols: []string{safeSourcePK}, onDelete: entity.OnDeleteCascade},
			{cols: []string{safeTargetKey}, target: target, refTable: safeTargetTable, refCols: []string{safeTargetPK}, onDelete: entity.OnDeleteCascade},
		},
	}, nil
}

func migratePivotTables(ctx context.Context, exec execQueryer, ordered []*entity.Entity, all map[string]*entity.Entity, dialect Dialect) error {
	for ent, rel := range generatedPivots(ordered, all) {
		spec, err := buildPivotSpec(ent, rel, all, dialect)
		if err != nil {
			return err
		}
		cols, err := readLiveColumnsQ(ctx, exec, rel.Through, dialect)
		if err != nil {
			return fmt.Errorf("read pivot table %s: %w", rel.Through, err)
		}
		if len(cols) > 0 {
			if err := checkPivotColumns(spec, cols, dialect); err != nil {
				return err
			}
			if err := checkLivePivotConstraints(ctx, exec, spec, all, dialect); err != nil {
				return err
			}
		}
		if _, err := exec.ExecContext(ctx, spec.DDL); err != nil {
			return fmt.Errorf("create pivot table %s: %w", rel.Through, err)
		}
		if _, err := exec.ExecContext(ctx, spec.IndexDDL); err != nil {
			return fmt.Errorf("create index on pivot table %s(%s): %w", rel.Through, rel.ForeignKeyTarget, err)
		}
	}
	return nil
}
