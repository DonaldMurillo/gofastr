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
	Skipped   []string           // erasers skipped because their identity could not be resolved (idempotent re-run)
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

	// Resolve every non-user-id identity ONCE, before the write transaction
	// opens (a single-connection pool deadlocks on a read inside the tx). An
	// eraser declaring an identity with no resolver fails loud here; an
	// identity whose value cannot be resolved is skipped, not failed.
	resolved, skipped, err := a.resolveEraserIdentities(ctx, dialect, userID, erasers)
	if err != nil {
		return EraseReport{}, err
	}

	if cfg.dryRun {
		rep, derr := a.eraseDryRun(ctx, dialect, userID, cfg, ents, resolved)
		if derr == nil {
			rep.Skipped = skipped
		}
		return rep, derr
	}
	rep, werr := a.eraseWrite(ctx, dialect, userID, cfg, ents, resolved)
	if werr == nil {
		rep.Skipped = skipped
	}
	return rep, werr
}

// resolvedEraser pairs a registered eraser with the value its match column is
// bound against. For IdentityUserID (the default) the value IS the user id;
// for a non-default identity it is the value resolved once at erase time
// through the matching DataIdentityResolver.
type resolvedEraser struct {
	eraser datexport.DataEraser
	value  string
}

// resolveEraserIdentities resolves the bound match value for every eraser,
// ONCE, before the write transaction opens (a single-connection pool deadlocks
// on a read inside the tx). It is the identity-resolver seam:
//
//   - IdentityUserID (the default): the value is the user id, unchanged. Every
//     existing registration behaves exactly as before.
//   - A non-default identity with NO registered resolver: FAIL LOUD. An erasure
//     that cannot reach a declared table is incomplete, and silently-incomplete
//     is the failure mode this primitive exists to prevent.
//   - A non-default identity whose resolver runs but finds no row (the user row
//     is already gone on an idempotent re-run) or whose table is absent (host
//     renamed it): the eraser is SKIPPED (added to skipped, not returned)
//     rather than failed. An unresolvable identity means "nothing left to
//     match".
func (a *App) resolveEraserIdentities(ctx context.Context, dialect migrate.Dialect, userID string, erasers []datexport.DataEraser) (resolved []resolvedEraser, skipped []string, err error) {
	for _, e := range erasers {
		if e.Identity == datexport.IdentityUserID {
			resolved = append(resolved, resolvedEraser{eraser: e, value: userID})
			continue
		}
		r, ok := datexport.ResolveIdentity(e.Identity)
		if !ok {
			return nil, nil, fmt.Errorf("framework: erase %q declares identity %d but no resolver is registered — erasure would be incomplete", e.Name, e.Identity)
		}
		val, found, rerr := a.resolveIdentityValue(ctx, dialect, userID, r)
		if rerr != nil {
			return nil, nil, fmt.Errorf("framework: erase %q: resolve identity: %w", e.Name, rerr)
		}
		if !found {
			skipped = append(skipped, e.Name)
			continue
		}
		resolved = append(resolved, resolvedEraser{eraser: e, value: val})
	}
	return resolved, skipped, nil
}

// resolveIdentityValue runs the declarative resolver —
//
//	SELECT ValueColumn FROM Table WHERE IDColumn = userID
//
// against a.DB, NOT inside the transaction, and reports whether a row matched.
// An absent resolver table is treated as "not found" (skip, not fail): the host
// may have renamed the user table, mirroring how an absent eraser table is
// skipped. Table/IDColumn/ValueColumn are MustIdent-guarded (decision E3); the
// user id is a $n bound argument. A NULL or blank value is treated as
// unresolvable (the eraser is skipped) — an empty identity cannot usefully
// match rows.
func (a *App) resolveIdentityValue(ctx context.Context, dialect migrate.Dialect, userID string, r datexport.DataIdentityResolver) (string, bool, error) {
	exists, err := tableExists(ctx, a.DB, r.Table, dialect)
	if err != nil {
		return "", false, fmt.Errorf("probe resolver table %q: %w", r.Table, err)
	}
	if !exists {
		return "", false, nil
	}
	qt := query.QuoteIdent(query.MustIdent(r.Table))
	qID := query.QuoteIdent(query.MustIdent(r.IDColumn))
	qVal := query.QuoteIdent(query.MustIdent(r.ValueColumn))
	var val sql.NullString
	err = a.DB.QueryRowContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1", qVal, qt, qID), userID).Scan(&val)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !val.Valid || val.String == "" {
		return "", false, nil
	}
	return val.String, true, nil
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
// the probe waited for one). Erasers arrive already identity-resolved (see
// resolveEraserIdentities): each carries the value its match column is bound
// against — the user id, or a resolved identity like email.
func (a *App) eraseWrite(ctx context.Context, dialect migrate.Dialect, userID string, cfg eraseConfig, ents []eraseEntitySource, erasers []resolvedEraser) (EraseReport, error) {
	report := EraseReport{DryRun: false}

	liveErasers := make([]resolvedEraser, 0, len(erasers))
	for _, e := range erasers {
		exists, err := tableExists(ctx, a.DB, e.eraser.Table, dialect)
		if err != nil {
			return report, fmt.Errorf("framework: erase probe %q: %w", e.eraser.Table, err)
		}
		if !exists {
			fmt.Fprintf(os.Stderr, "framework: erase: table %q absent, skipping\n", e.eraser.Table)
			continue
		}
		liveErasers = append(liveErasers, e)
	}
	auditExists, err := tableExists(ctx, a.DB, cfg.auditTable, dialect)
	if err != nil {
		return report, fmt.Errorf("framework: erase probe audit: %w", err)
	}
	// Column probes run BEFORE BeginTx: a single-connection pool (SQLite in
	// tests, MaxOpenConns(1) in production) would deadlock reading schema
	// while the transaction holds the only connection.
	for _, s := range ents {
		if err := requireColumn(ctx, a.DB, s.table, s.owner, dialect); err != nil {
			return report, fmt.Errorf("framework: erase entity %q: %w", s.name, err)
		}
	}
	for _, e := range liveErasers {
		if err := requireColumn(ctx, a.DB, e.eraser.Table, e.eraser.Column, dialect); err != nil {
			return report, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
		}
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

	// Battery plane: registered erasers, each matched against its resolved
	// value (user id, or a resolved identity like email).
	for _, e := range liveErasers {
		switch e.eraser.Mode {
		case datexport.EraseDelete:
			n, derr := eraseDelete(ctx, tx, e.eraser.Table, e.eraser.Column, e.value)
			if derr != nil {
				return report, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, derr)
			}
			report.Batteries = append(report.Batteries, EraseTableResult{
				Name: e.eraser.Name, Source: e.eraser.Source, Table: e.eraser.Table, Mode: "delete", RowsAffected: n,
			})
		case datexport.EraseAnonymize:
			ts := e.eraser.Tombstone
			if ts == "" {
				ts = "[erased]"
			}
			n, derr := eraseAnonymize(ctx, tx, e.eraser, e.value, ts)
			if derr != nil {
				return report, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, derr)
			}
			report.Batteries = append(report.Batteries, EraseTableResult{
				Name: e.eraser.Name, Source: e.eraser.Source, Table: e.eraser.Table, Mode: "anonymize", RowsAffected: n,
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
// Erasers arrive already identity-resolved (see resolveEraserIdentities): each
// is counted against its resolved value, taking the SAME resolution path as a
// real erase.
func (a *App) eraseDryRun(ctx context.Context, dialect migrate.Dialect, userID string, cfg eraseConfig, ents []eraseEntitySource, erasers []resolvedEraser) (EraseReport, error) {
	report := EraseReport{DryRun: true}

	for _, s := range ents {
		if err := requireColumn(ctx, a.DB, s.table, s.owner, dialect); err != nil {
			return report, fmt.Errorf("framework: erase entity %q: %w", s.name, err)
		}
		n, err := eraseCount(ctx, a.DB, s.table, s.owner, userID)
		if err != nil {
			return report, fmt.Errorf("framework: erase entity %q: %w", s.name, err)
		}
		report.Entities = append(report.Entities, EraseTableResult{
			Name: s.name, Source: "entity", Table: s.table, Mode: "delete", RowsAffected: n,
		})
	}
	for _, e := range erasers {
		exists, err := tableExists(ctx, a.DB, e.eraser.Table, dialect)
		if err != nil {
			return report, fmt.Errorf("framework: erase probe %q: %w", e.eraser.Table, err)
		}
		if !exists {
			continue
		}
		if err := requireColumn(ctx, a.DB, e.eraser.Table, e.eraser.Column, dialect); err != nil {
			return report, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
		}
		mode := "delete"
		if e.eraser.Mode == datexport.EraseAnonymize {
			mode = "anonymize"
		}
		n, err := eraseCount(ctx, a.DB, e.eraser.Table, e.eraser.Column, e.value)
		if err != nil {
			return report, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
		}
		report.Batteries = append(report.Batteries, EraseTableResult{
			Name: e.eraser.Name, Source: e.eraser.Source, Table: e.eraser.Table, Mode: mode, RowsAffected: n,
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

// requireColumn fails when table has no column named col. SQLite reads a
// double-quoted unknown identifier as a STRING LITERAL, so
// `DELETE FROM "t" WHERE "owner_id" = $1` against a table with no owner_id
// deletes nothing and reports success — an erasure that silently erases
// nothing is the one outcome a right-to-be-forgotten primitive must never
// produce. Postgres rejects the unknown column itself; this makes both
// engines behave the same way.
func requireColumn(ctx context.Context, db *sql.DB, table, col string, dialect migrate.Dialect) error {
	live, err := migrate.ReadLiveColumns(ctx, db, table, dialect)
	if err != nil {
		return fmt.Errorf("read columns of %q: %w", table, err)
	}
	if len(live) == 0 {
		return nil // table absent or unreadable — the caller's existence probe owns that case
	}
	for name := range live {
		if strings.EqualFold(name, col) {
			return nil
		}
	}
	return fmt.Errorf("table %q has no column %q — erasure would silently match nothing", table, col)
}
