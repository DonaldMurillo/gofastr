package framework

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Data erasure — the right-to-be-forgotten half of the data lifecycle.
//
// EraseUserData mirrors ExportData's two-plane design: the entity registry
// (owner-scoped entities) PLUS the datexport registry (battery-declared
// erasers), so an erasure reaches the same tables an export does. ExportData
// is the "let me have my data" primitive (GDPR portability); EraseUserData is
// the "erase me" primitive (GDPR erasure).
//
// # Semantics (design decision E1)
//
// Erasure means erasure. The entity plane HARD-deletes every row owned by the
// user — including soft-deleted rows. A raw DELETE bypasses the deleted_at
// filter, so a row the user previously "deleted" is now actually expunged.
// There is no soft-delete respect, no tombstone, and no undo.
//
// # Audit retention (design decision E2)
//
// The audit_log table is RETAINED — it is the compliance record of who did
// what. Instead of deleting rows, the built-in audit plane anonymizes the
// actor_id of every row the erased user acted from (actor_id = userID →
// "[erased]"). record_id is left intact: it is heterogeneous (a resource id
// for CRUD events, sometimes a user id for auth events) and records WHAT was
// acted on, which is legitimate audit content. This is the industry-standard
// posture: keep the trail, cut the personal link.
//
// # SQL safety (design decision E3)
//
// Same firewall as export: every table/column name is registry- or eraser-
// derived and passes through query.MustIdent before query.QuoteIdent; the user
// id and the tombstone are $n bound arguments. A misconfigured eraser fails
// loud at MustIdent rather than interpolating an unsafe name.
//
// # Transaction & idempotency (design decision E4)
//
// The write phase runs inside a single transaction (rollback on any error),
// mirroring ImportData. EraseUserData is idempotent: a second call for the
// same user matches zero rows and returns a zero report without error.

// EraseOption configures EraseUserData.
type EraseOption func(*eraseConfig)

type eraseConfig struct {
	dryRun         bool
	auditTable     string
	auditTombstone string
}

// WithEraseDryRun switches EraseUserData to count-only mode: the returned
// report carries the rows that WOULD be affected, but no rows are deleted or
// scrubbed. Use it for compliance review before committing to an erasure.
func WithEraseDryRun() EraseOption {
	return func(c *eraseConfig) { c.dryRun = true }
}

// WithEraseAuditTable overrides the audit table anonymized by the built-in
// audit plane. Defaults to "audit_log" (matching EnsureAuditTable). Set this
// only if your app renamed the audit table via AuditConfig.Table.
func WithEraseAuditTable(table string) EraseOption {
	return func(c *eraseConfig) { c.auditTable = table }
}

// WithEraseAuditTombstone overrides the value written to actor_id when the
// audit plane anonymizes a row. Defaults to "[erased]".
func WithEraseAuditTombstone(ts string) EraseOption {
	return func(c *eraseConfig) { c.auditTombstone = ts }
}

// EraseTableResult is one table's contribution to an EraseReport.
type EraseTableResult struct {
	Name         string // entity/eraser name (report label)
	Source       string // "entity" | battery source | "audit"
	Table        string // physical table
	Mode         string // "delete" | "anonymize"
	RowsAffected int
}

// EraseReport is the structured summary returned by EraseUserData.
type EraseReport struct {
	DryRun    bool
	Entities  []EraseTableResult // owner-scoped entity tables (always delete)
	Batteries []EraseTableResult // registered erasers (delete or anonymize)
	Audit     *EraseTableResult  // built-in audit anonymization; nil if the audit table is absent
}

// TotalErased returns the sum of every RowsAffected across all planes. In
// dry-run mode this is the count that WOULD be affected.
func (r EraseReport) TotalErased() int {
	n := 0
	for i := range r.Entities {
		n += r.Entities[i].RowsAffected
	}
	for i := range r.Batteries {
		n += r.Batteries[i].RowsAffected
	}
	if r.Audit != nil {
		n += r.Audit.RowsAffected
	}
	return n
}

// eraseEntitySource describes one owner-scoped physical table to erase.
type eraseEntitySource struct {
	name  string
	table string
	owner string
}

// EraseUserData is the right-to-be-forgotten primitive: it expunges every row
// owned by userID across owner-scoped entities and registered battery tables,
// and anonymizes the user's actor reference in the audit trail. See the
// package comment for the erasure, audit-retention, SQL-safety, and
// idempotency contracts.
func (a *App) EraseUserData(ctx context.Context, userID string, opts ...EraseOption) (EraseReport, error) {
	if a == nil {
		return EraseReport{}, fmt.Errorf("framework: EraseUserData on nil App")
	}
	if a.DB == nil {
		return EraseReport{}, fmt.Errorf("framework: EraseUserData requires App.DB")
	}
	// A blank id is never a real user: `WHERE owner = ''` matches every
	// unowned row, so a caller that failed to resolve its principal would
	// erase them instead of erroring.
	if strings.TrimSpace(userID) == "" {
		return EraseReport{}, fmt.Errorf("framework: EraseUserData requires a non-empty user id")
	}
	cfg := eraseConfig{auditTable: "audit_log", auditTombstone: "[erased]"}
	for _, o := range opts {
		o(&cfg)
	}

	dialect := migrate.DetectDialect(a.DB)
	ents := a.collectEraseEntitySources()
	erasers := datexport.AllErasers()

	if cfg.dryRun {
		return a.eraseDryRun(ctx, dialect, userID, cfg, ents, erasers)
	}
	return a.eraseWrite(ctx, dialect, userID, cfg, ents, erasers)
}

// collectEraseEntitySources enumerates every owner-scoped PHYSICAL table in
// the entity registry, deduped by table (mirroring collectSources' table
// collapse). Each yields {name, table, owner column}. Entities without an
// OwnerField are skipped — they hold no per-user data. Version-union comes
// from migrate.UnionEntities, so two versions of one name produce one source
// for their shared table (same contract as export).
func (a *App) collectEraseEntitySources() []eraseEntitySource {
	var out []eraseEntitySource
	if a.Registry == nil {
		return out
	}
	merged := migrate.UnionEntities(a.registryView())
	names := make([]string, 0, len(merged))
	for n := range merged {
		names = append(names, n)
	}
	sort.Strings(names)

	// One owner column per physical table; the lex-first entity name labels it
	// (matching collectSources' lex-first naming).
	tableOwner := map[string]string{}
	tableName := map[string]string{}
	for _, n := range names {
		ent := merged[n]
		if ent == nil || ent.Config.Scope == nil || ent.Config.Scope.OwnerField == "" {
			continue
		}
		table := ent.GetTable()
		if _, first := tableOwner[table]; !first {
			tableOwner[table] = ent.Config.Scope.OwnerField
			tableName[table] = n
		}
	}
	tables := make([]string, 0, len(tableOwner))
	for t := range tableOwner {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	tables = orderChildrenFirst(tables, merged, names)
	for _, t := range tables {
		out = append(out, eraseEntitySource{name: tableName[t], table: t, owner: tableOwner[t]})
	}
	return out
}

// orderChildrenFirst sorts erasable tables so a table holding a foreign key
// is deleted before the table it points at. The generated schema has no ON
// DELETE CASCADE, so deleting a parent first fails the constraint and rolls
// the whole erasure back. Input order (lexical) is preserved between tables
// with no dependency, keeping the result deterministic; a relation cycle
// degrades to the input order rather than looping.
func orderChildrenFirst(tables []string, merged map[string]*entity.Entity, names []string) []string {
	erasable := make(map[string]bool, len(tables))
	for _, t := range tables {
		erasable[t] = true
	}
	// parents[child] = the tables that child references via BelongsTo — the
	// child holds the FK column (RelManyToOne, i.e. BelongsTo), so it must
	// be deleted first.
	parents := map[string][]string{}
	for _, n := range names {
		ent := merged[n]
		if ent == nil || !erasable[ent.GetTable()] {
			continue
		}
		child := ent.GetTable()
		for _, rel := range ent.Config.Relations {
			if rel.Type != entity.RelManyToOne {
				continue
			}
			target := merged[rel.Entity]
			if target == nil || !erasable[target.GetTable()] || target.GetTable() == child {
				continue
			}
			parents[child] = append(parents[child], target.GetTable())
		}
	}
	if len(parents) == 0 {
		return tables
	}
	var ordered []string
	state := map[string]int{} // 0 unvisited, 1 in-progress, 2 done
	var visit func(string)
	visit = func(t string) {
		switch state[t] {
		case 1, 2: // in-progress means a cycle: stop rather than loop
			return
		}
		state[t] = 1
		for _, child := range childrenOf(t, parents) {
			visit(child)
		}
		state[t] = 2
		ordered = append(ordered, t)
	}
	for _, t := range tables {
		visit(t)
	}
	return ordered
}

// childrenOf returns the tables that reference parent, in the deterministic
// order their names sort.
func childrenOf(parent string, parents map[string][]string) []string {
	var out []string
	for child, ps := range parents {
		for _, p := range ps {
			if p == parent {
				out = append(out, child)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// eraseWrite performs the real erasure inside one transaction. Existence
// probes run against a.DB BEFORE BeginTx so a single-connection SQLite pool
// does not deadlock (the tx would otherwise hold the only connection while
// the probe waited for one).
func (a *App) eraseWrite(ctx context.Context, dialect migrate.Dialect, userID string, cfg eraseConfig, ents []eraseEntitySource, erasers []datexport.DataEraser) (EraseReport, error) {
	report := EraseReport{DryRun: false}

	liveErasers := make([]datexport.DataEraser, 0, len(erasers))
	for _, e := range erasers {
		exists, err := tableExists(ctx, a.DB, e.Table, dialect)
		if err != nil {
			return report, fmt.Errorf("framework: erase probe %q: %w", e.Table, err)
		}
		if !exists {
			fmt.Fprintf(os.Stderr, "framework: erase: table %q absent, skipping\n", e.Table)
			continue
		}
		liveErasers = append(liveErasers, e)
	}
	auditExists, err := tableExists(ctx, a.DB, cfg.auditTable, dialect)
	if err != nil {
		return report, fmt.Errorf("framework: erase probe audit: %w", err)
	}

	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return report, fmt.Errorf("framework: erase: begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Entity plane: hard-delete by owner column (no soft-delete filter).
	for _, s := range ents {
		n, err := eraseDelete(ctx, tx, s.table, s.owner, userID)
		if err != nil {
			return report, fmt.Errorf("framework: erase entity %q: %w", s.name, err)
		}
		report.Entities = append(report.Entities, EraseTableResult{
			Name: s.name, Source: "entity", Table: s.table, Mode: "delete", RowsAffected: n,
		})
	}

	// Battery plane: registered erasers.
	for _, e := range liveErasers {
		switch e.Mode {
		case datexport.EraseDelete:
			n, derr := eraseDelete(ctx, tx, e.Table, e.Column, userID)
			if derr != nil {
				return report, fmt.Errorf("framework: erase %q: %w", e.Name, derr)
			}
			report.Batteries = append(report.Batteries, EraseTableResult{
				Name: e.Name, Source: e.Source, Table: e.Table, Mode: "delete", RowsAffected: n,
			})
		case datexport.EraseAnonymize:
			ts := e.Tombstone
			if ts == "" {
				ts = "[erased]"
			}
			n, derr := eraseAnonymize(ctx, tx, e, userID, ts)
			if derr != nil {
				return report, fmt.Errorf("framework: erase %q: %w", e.Name, derr)
			}
			report.Batteries = append(report.Batteries, EraseTableResult{
				Name: e.Name, Source: e.Source, Table: e.Table, Mode: "anonymize", RowsAffected: n,
			})
		}
	}

	// Audit plane: anonymize actor_id (built-in, framework-owned table).
	if auditExists {
		n, aerr := eraseAnonymizeRaw(ctx, tx, cfg.auditTable, "actor_id", userID, cfg.auditTombstone, "actor_id")
		if aerr != nil {
			return report, fmt.Errorf("framework: erase audit: %w", aerr)
		}
		report.Audit = &EraseTableResult{
			Name: cfg.auditTable, Source: "audit", Table: cfg.auditTable,
			Mode: "anonymize", RowsAffected: n,
		}
	}

	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("framework: erase: commit: %w", err)
	}
	committed = true
	return report, nil
}

// eraseDryRun counts every row that WOULD be affected without writing. No
// transaction (read-only); absent tables contribute nothing and are skipped.
func (a *App) eraseDryRun(ctx context.Context, dialect migrate.Dialect, userID string, cfg eraseConfig, ents []eraseEntitySource, erasers []datexport.DataEraser) (EraseReport, error) {
	report := EraseReport{DryRun: true}

	for _, s := range ents {
		n, err := eraseCount(ctx, a.DB, s.table, s.owner, userID)
		if err != nil {
			return report, fmt.Errorf("framework: erase entity %q: %w", s.name, err)
		}
		report.Entities = append(report.Entities, EraseTableResult{
			Name: s.name, Source: "entity", Table: s.table, Mode: "delete", RowsAffected: n,
		})
	}
	for _, e := range erasers {
		exists, err := tableExists(ctx, a.DB, e.Table, dialect)
		if err != nil {
			return report, fmt.Errorf("framework: erase probe %q: %w", e.Table, err)
		}
		if !exists {
			continue
		}
		mode := "delete"
		if e.Mode == datexport.EraseAnonymize {
			mode = "anonymize"
		}
		n, err := eraseCount(ctx, a.DB, e.Table, e.Column, userID)
		if err != nil {
			return report, fmt.Errorf("framework: erase %q: %w", e.Name, err)
		}
		report.Batteries = append(report.Batteries, EraseTableResult{
			Name: e.Name, Source: e.Source, Table: e.Table, Mode: mode, RowsAffected: n,
		})
	}
	auditExists, err := tableExists(ctx, a.DB, cfg.auditTable, dialect)
	if err != nil {
		return report, fmt.Errorf("framework: erase probe audit: %w", err)
	}
	if auditExists {
		n, err := eraseCount(ctx, a.DB, cfg.auditTable, "actor_id", userID)
		if err != nil {
			return report, fmt.Errorf("framework: erase audit: %w", err)
		}
		report.Audit = &EraseTableResult{
			Name: cfg.auditTable, Source: "audit", Table: cfg.auditTable,
			Mode: "anonymize", RowsAffected: n,
		}
	}
	return report, nil
}

// eraseDelete executes DELETE FROM table WHERE col = userID inside tx and
// returns the number of rows deleted. Identifiers are MustIdent-guarded; userID
// is a bound argument.
func eraseDelete(ctx context.Context, tx *sql.Tx, table, col, userID string) (int, error) {
	qt := query.QuoteIdent(query.MustIdent(table))
	qc := query.QuoteIdent(query.MustIdent(col))
	res, err := tx.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE %s = $1", qt, qc), userID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// eraseAnonymize runs the UPDATE declared by a registered DataEraser: every
// ScrubColumn AND the match Column are set to tombstone on rows where
// Column = userID. The match column is scrubbed too — it IS the user-id
// reference being cut, and including it makes the UPDATE idempotent by count:
// once the match column holds the tombstone, a re-run matches zero rows
// (matching the DELETE plane's natural idempotency). Returns the number of
// rows updated.
func eraseAnonymize(ctx context.Context, tx *sql.Tx, e datexport.DataEraser, userID, tombstone string) (int, error) {
	if len(e.ScrubColumns) == 0 {
		return 0, fmt.Errorf("eraser %q: EraseAnonymize requires ScrubColumns", e.Name)
	}
	scrub := append([]string(nil), e.ScrubColumns...)
	dedup := make(map[string]bool, len(scrub)+1)
	for _, c := range scrub {
		dedup[strings.ToLower(c)] = true
	}
	if !dedup[strings.ToLower(e.Column)] {
		scrub = append(scrub, e.Column)
	}
	return eraseAnonymizeRaw(ctx, tx, e.Table, e.Column, userID, tombstone, scrub...)
}

// eraseAnonymizeRaw is the shared UPDATE builder for the battery-plane and the
// built-in audit plane. Each scrub column AND the match column are
// MustIdent-guarded; tombstone and userID are bound arguments.
func eraseAnonymizeRaw(ctx context.Context, tx *sql.Tx, table, matchCol, userID, tombstone string, scrubCols ...string) (int, error) {
	qt := query.QuoteIdent(query.MustIdent(table))
	qMatch := query.QuoteIdent(query.MustIdent(matchCol))
	sets := make([]string, len(scrubCols))
	for i, c := range scrubCols {
		// $1 is the tombstone, reused for every scrubbed column.
		sets[i] = query.QuoteIdent(query.MustIdent(c)) + " = $1"
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $2",
		qt, strings.Join(sets, ", "), qMatch)
	res, err := tx.ExecContext(ctx, stmt, tombstone, userID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// eraseCount returns the number of rows in table where col equals userID. Used
// by dry-run to project an erasure without writing.
func eraseCount(ctx context.Context, db *sql.DB, table, col, userID string) (int, error) {
	qt := query.QuoteIdent(query.MustIdent(table))
	qc := query.QuoteIdent(query.MustIdent(col))
	var n int
	err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = $1", qt, qc), userID).Scan(&n)
	return n, err
}
