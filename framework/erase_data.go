package framework

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Data erasure: the right-to-be-forgotten half of the data lifecycle.
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
// user, including soft-deleted rows. A raw DELETE bypasses the deleted_at
// filter, so a row the user previously "deleted" is now actually expunged.
// There is no soft-delete respect, no tombstone, and no undo.
//
// # Cascade (design decision E5)
//
// Erasure follows foreign keys the way ON DELETE CASCADE would. A row whose
// BelongsTo key points at a row being erased is erased too, whether or not
// its own table is owner-scoped and whoever owns it: the items of a user's
// list, another user's comment on the user's post, the replies under that
// comment, and the pivot rows of a many_to_many. The walk is recursive,
// follows composite keys column for column, and deletes children before
// parents in the same transaction, so the generated constraints (which carry
// no ON DELETE action) never refuse it. Where no order works (two tables
// naming each other, or a RESTRICT self-reference), the references between
// erased rows are emptied before the deletes; a NOT NULL one fails the
// erasure. A row whose primary key holds a NULL (SQLite allows it outside
// an INTEGER key) cannot be told apart, and erasing one is refused. A
// relation declared ON DELETE SET
// NULL is not followed: the database detaches that child and it survives.
// A relation declared ON DELETE RESTRICT or NO ACTION is not followed either:
// the developer said a child row blocks the parent's delete, so when a row
// the erasure does not remove itself names a row it would, EraseUserData
// (dry run included) returns an error naming the relation and the child
// table, and erases nothing. The user's own child rows do not block: they
// are deleted first. An unset OnDelete and CASCADE are followed.
// Unmanaged entities (views, host-owned tables) are not reached by the
// cascade.
//
// # Audit retention (design decision E2)
//
// The audit_log table is RETAINED, it is the compliance record of who did
// what. Instead of deleting rows, the built-in audit plane anonymizes the
// actor_id of every row the erased user acted from (actor_id = userID ->
// "[erased]"), and replaces the diff of those rows, and of every row that
// records an erased row (its entity and record_id), with the JSON marker
// {"erased":true}: a diff is a copy of the row, and erasing the row while
// keeping its copy erases nothing. record_id is left intact: it is
// heterogeneous (a resource id for CRUD events, sometimes a user id for auth
// events) and records WHAT was acted on, which is legitimate audit content.
// This is the industry-standard posture: keep the trail, cut the personal
// link. The audit table needs an actor_id column. A custom table
// (WithEraseAuditTable) with no diff column has no diff to scrub, and one with
// no entity or record_id column cannot name the rows that record an erased
// row, so those scrubs are skipped there.
//
// # SQL safety (design decision E3)
//
// Same firewall as export: every table/column name is registry- or eraser-
// derived and passes through query.MustIdent before query.QuoteFolded; the user
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
// The dry run performs the erasure inside a transaction it rolls back, so
// its counts, and any constraint that refuses the erasure, are the real
// run's; it holds the same row locks while it runs, and deletes no stored
// object.
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
//
// Source is "entity" for the rows a user owns, "cascade" for the rows erased
// because their foreign key named an erased row, the eraser's battery source
// for a registered eraser, and "audit" for the audit plane.
type EraseTableResult struct {
	Name         string // entity/eraser name (report label); the table itself for a pivot
	Source       string // "entity" | "cascade" | battery source | "audit"
	Table        string // physical table
	Mode         string // "delete" | "anonymize"
	RowsAffected int
}

// EraseReport is the structured summary returned by EraseUserData.
type EraseReport struct {
	DryRun   bool
	Entities []EraseTableResult // owner-scoped entity tables: the rows the user owns (always delete)
	// Cascaded lists every table the cascade reaches (decision E5),
	// owner-scoped or not, with the rows deleted because a foreign key named
	// an erased row. A row the user owns counts in Entities, never here too.
	Cascaded  []EraseTableResult
	Batteries []EraseTableResult // registered erasers (delete or anonymize)
	// Audit is the built-in audit plane; nil if the audit table is absent.
	// RowsAffected counts the rows whose actor was anonymized plus the other
	// rows whose diff was replaced because they record an erased row.
	Audit *EraseTableResult
	// Stores lists the registered datexport.StoreErasers that ran: stores
	// outside the database, such as a semantic index's files or a Redis
	// queue. They run after the SQL erasure commits; one that fails is not
	// listed, and EraseUserData returns an error naming it.
	Stores  []EraseTableResult
	Skipped []string // erasers skipped because their identity could not be resolved (idempotent re-run)
}

// erasedAuditDiff replaces the diff of an audit row that names the erased
// user or records an erased row. It is JSON, so a reader that decodes the
// column as {"old":…,"new":…} still decodes it, to an empty change.
const erasedAuditDiff = `{"erased":true}`

// TotalErased returns the sum of every RowsAffected across all planes. In
// dry-run mode this is the count that WOULD be affected.
func (r EraseReport) TotalErased() int {
	n := 0
	for i := range r.Entities {
		n += r.Entities[i].RowsAffected
	}
	for i := range r.Cascaded {
		n += r.Cascaded[i].RowsAffected
	}
	for i := range r.Batteries {
		n += r.Batteries[i].RowsAffected
	}
	if r.Audit != nil {
		n += r.Audit.RowsAffected
	}
	for i := range r.Stores {
		n += r.Stores[i].RowsAffected
	}
	return n
}

// EraseUserData is the right-to-be-forgotten primitive: it expunges every row
// owned by userID across owner-scoped entities and registered battery tables,
// every row whose foreign key names one of those rows, and anonymizes the
// user's actor reference and data in the audit trail. See the package
// comment for the erasure, cascade, audit-retention, SQL-safety, and
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
	erasers := datexport.AllErasers()

	// Resolve every non-user-id identity ONCE, before the write transaction
	// opens (a single-connection pool deadlocks on a read inside the tx). An
	// eraser declaring an identity with no resolver fails loud here; an
	// identity whose value cannot be resolved is skipped, not failed. It runs
	// before the plan, which seeds the cascade from the delete erasers.
	resolved, skipped, err := a.resolveEraserIdentities(ctx, dialect, userID, erasers)
	if err != nil {
		return EraseReport{}, err
	}
	plan, err := a.planErase(ctx, dialect, resolved)
	if err != nil {
		return EraseReport{}, err
	}

	rep, werr := a.eraseWrite(ctx, dialect, userID, cfg, plan, resolved, cfg.dryRun)
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
	// table is the eraser's table as the catalog spells it
	// (physicalTable), set once the table is found.
	table string
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
			resolved = append(resolved, resolvedEraser{eraser: e, value: e.ValuePrefix + userID})
			continue
		}
		r, ok := datexport.ResolveIdentity(e.Identity)
		if !ok {
			return nil, nil, fmt.Errorf("framework: erase %q declares identity %d but no resolver is registered: erasure would be incomplete", e.Name, e.Identity)
		}
		val, found, rerr := a.resolveIdentityValue(ctx, dialect, userID, r)
		if rerr != nil {
			return nil, nil, fmt.Errorf("framework: erase %q: resolve identity: %w", e.Name, rerr)
		}
		if !found {
			skipped = append(skipped, e.Name)
			continue
		}
		resolved = append(resolved, resolvedEraser{eraser: e, value: e.ValuePrefix + val})
	}
	return resolved, skipped, nil
}

// resolveIdentityValue runs the declarative resolver:
//
//	SELECT ValueColumn FROM Table WHERE IDColumn = userID
//
// against a.DB, NOT inside the transaction, and reports whether a row matched.
// An absent resolver table is treated as "not found" (skip, not fail): the host
// may have renamed the user table, mirroring how an absent eraser table is
// skipped. Table/IDColumn/ValueColumn are MustIdent-guarded (decision E3); the
// user id is a $n bound argument. A NULL or blank value is treated as
// unresolvable (the eraser is skipped), an empty identity cannot usefully
// match rows.
func (a *App) resolveIdentityValue(ctx context.Context, dialect migrate.Dialect, userID string, r datexport.DataIdentityResolver) (string, bool, error) {
	phys, exists, err := physicalTable(ctx, a.DB, r.Table, dialect, false)
	if err != nil {
		return "", false, fmt.Errorf("probe resolver table %q: %w", r.Table, err)
	}
	if !exists {
		return "", false, nil
	}
	qt := quotePhysical(phys)
	qID := query.QuoteFolded(query.MustIdent(r.IDColumn))
	qVal := query.QuoteFolded(query.MustIdent(r.ValueColumn))
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

// ---- The relation graph: the tables the data planes walk and the foreign
// keys between them. Erasure walks it children first, a restore parents
// first.

// dataTable is one physical table: the entities stored in it collapsed onto
// one (the version union and every name sharing the table), or a
// many_to_many pivot table AutoMigrate creates.
type dataTable struct {
	name      string   // label: the lex-first entity name, or the pivot table
	table     string   // physical table
	key       []string // primary-key columns
	entities  []string // entity names stored here, for audit lookups; none for a pivot
	pivot     bool
	unmanaged bool // every entity on the table is Unmanaged (a view or a host-owned table)
	// owner and ownerName come from the lex-first owner-scoped entity on the
	// table; "" when none is, and the table then holds no rows of its own
	// for erasure.
	owner     string
	ownerName string
	// fileCols are the table's schema.Image / schema.File columns, whose
	// values are storage keys. The erase plane reads them before it deletes
	// the rows so the objects can be removed too; see eraseStoredObjects.
	fileCols []string
	// variantCols are the sibling "<field>_variants" JSON columns the image
	// pipeline writes renditions into (crud's applyDerivedColumns). Each
	// holds an array of {storage_ref}, and every rendition is as much the
	// user's file as the original.
	variantCols []string
}

// fkEdge is one foreign key: child.cols reference parent.key, column for
// column.
type fkEdge struct {
	child, parent string
	cols, key     []string
	setNull       bool // ON DELETE SET NULL: the database detaches the child itself
	// blocks is RESTRICT or NO ACTION, as declared: a child row refuses its
	// parent's delete, so erasure never follows the edge, and refuses when a
	// row it does not erase itself names a row it would.
	blocks entity.OnDeleteAction
	label  string // "<entity>.<relation>", or the pivot table
}

// blockingAction returns action when it is one under which a child row
// refuses its parent's delete, and "" otherwise (unset, CASCADE, SET NULL).
func blockingAction(action entity.OnDeleteAction) entity.OnDeleteAction {
	if action == entity.OnDeleteRestrict || action == entity.OnDeleteNoAction {
		return action
	}
	return ""
}

// fkEdgeID dedupes an edge declared twice (two versions, two names on one
// table). Columns are identifiers and hold no comma.
type fkEdgeID struct{ child, parent, cols string }

type dataGraph struct {
	tables map[string]*dataTable
	sorted []string // table names, lexical
	edges  []fkEdge
}

// buildDataGraph reads the registry into the relation graph: one dataTable
// per physical table, one edge per BelongsTo, and two per generated pivot.
// A foreign key whose columns do not line up with the key it references is
// refused (AutoMigrate refuses it too, so no such constraint exists): a
// walk that skipped it would silently leave rows behind.
func (a *App) buildDataGraph() (*dataGraph, error) {
	g := &dataGraph{tables: map[string]*dataTable{}}
	if a.Registry == nil {
		return g, nil
	}
	merged := migrate.UnionEntities(a.registryView())
	names := slices.Sorted(maps.Keys(merged))
	for _, n := range names {
		ent := merged[n]
		if ent == nil {
			continue
		}
		t := ent.GetTable()
		dt := g.tables[t]
		if dt == nil {
			dt = &dataTable{name: n, table: t, key: ent.KeyColumns(), unmanaged: true}
			g.tables[t] = dt
		}
		dt.entities = append(dt.entities, n)
		if !ent.Config.Unmanaged {
			dt.unmanaged = false
		}
		if dt.owner == "" && ent.Config.Scope != nil && ent.Config.Scope.OwnerField != "" {
			dt.owner = ent.Config.Scope.OwnerField
			dt.ownerName = n
		}
		declared := map[string]bool{}
		for _, f := range ent.GetFields() {
			declared[f.Name] = true
		}
		for _, f := range ent.GetFields() {
			if f.Type != schema.Image && f.Type != schema.File {
				continue
			}
			dt.fileCols = appendUnique(dt.fileCols, f.Name)
			if v := f.Name + "_variants"; declared[v] {
				dt.variantCols = appendUnique(dt.variantCols, v)
			}
		}
	}
	seen := map[fkEdgeID]bool{}
	for _, n := range names {
		ent := merged[n]
		if ent == nil {
			continue
		}
		for _, rel := range ent.Config.Relations {
			if rel.Type != entity.RelManyToOne || len(rel.ForeignKey) == 0 {
				continue
			}
			target := merged[rel.Entity]
			if target == nil {
				continue // AutoMigrate refuses an unknown target, so no such key exists
			}
			key := target.KeyColumns()
			if len(rel.ForeignKey) != len(key) {
				return nil, fmt.Errorf("framework: entity %q relation %q: foreign key (%s) does not match %s's key (%s), so its rows cannot be followed",
					n, rel.Name, strings.Join(rel.ForeignKey, ", "), rel.Entity, strings.Join(key, ", "))
			}
			e := fkEdge{
				child: ent.GetTable(), parent: target.GetTable(),
				cols: slices.Clone(rel.ForeignKey), key: key,
				setNull: rel.OnDelete == entity.OnDeleteSetNull,
				blocks:  blockingAction(rel.OnDelete),
				label:   n + "." + rel.Name,
			}
			id := fkEdgeID{child: e.child, parent: e.parent, cols: strings.Join(e.cols, ",")}
			if seen[id] {
				continue
			}
			seen[id] = true
			g.edges = append(g.edges, e)
		}
	}
	for _, p := range migrate.GeneratedPivotTables(a.registryView()) {
		if g.tables[p.Table] != nil {
			continue
		}
		g.tables[p.Table] = &dataTable{name: p.Table, table: p.Table, key: []string{p.LocalKey, p.TargetFK}, pivot: true}
		g.edges = append(g.edges,
			fkEdge{child: p.Table, parent: p.SourceTable, cols: []string{p.LocalKey}, key: []string{p.SourceKey}, label: p.Table},
			fkEdge{child: p.Table, parent: p.TargetTable, cols: []string{p.TargetFK}, key: []string{p.TargetKey}, label: p.Table},
		)
	}
	g.sorted = slices.Sorted(maps.Keys(g.tables))
	return g, nil
}

// orderTables sorts tables so the two ends of every edge come out in
// dependency order: parents first for a restore (a child's insert needs its
// parent), children first for an erasure (a parent's delete needs its
// children gone; the generated schema has no ON DELETE CASCADE, so deleting
// a parent first fails the constraint and rolls the whole erasure back).
// Input order is preserved between tables with no dependency, keeping the
// result deterministic; a self-reference is ignored (its rows are one
// statement on erase and ordered row by row on restore), and a relation
// cycle degrades to the input order rather than looping.
func orderTables(tables []string, edges []fkEdge, parentsFirst bool) []string {
	in := make(map[string]bool, len(tables))
	for _, t := range tables {
		in[t] = true
	}
	// before[t] = the tables that must come out before t.
	before := map[string][]string{}
	for _, e := range edges {
		if e.child == e.parent || !in[e.child] || !in[e.parent] {
			continue
		}
		if parentsFirst {
			before[e.child] = append(before[e.child], e.parent)
		} else {
			before[e.parent] = append(before[e.parent], e.child)
		}
	}
	if len(before) == 0 {
		return tables
	}
	for t, deps := range before {
		slices.Sort(deps)
		before[t] = slices.Compact(deps)
	}
	ordered := make([]string, 0, len(tables))
	state := map[string]int{} // 0 unvisited, 1 in-progress, 2 done
	var visit func(string)
	visit = func(t string) {
		if state[t] != 0 { // in-progress means a cycle: stop rather than loop
			return
		}
		state[t] = 1
		for _, dep := range before[t] {
			visit(dep)
		}
		state[t] = 2
		ordered = append(ordered, t)
	}
	for _, t := range tables {
		visit(t)
	}
	return ordered
}

// ---- The erase plan.

// erasePlanTable is one table the entity plane erases from: a seed table
// (owner-scoped: the user's own rows) or one the cascade reaches.
type erasePlanTable struct {
	dt      *dataTable
	owner   string // owner column a seed table matches the user id against; "" when cascade-only
	tmp     string // session temp table holding the keys of the rows to erase
	reached bool   // a cascade edge leads here, so it is reported in Cascaded
	// seeds are the delete erasers on this table: their matches are staged
	// like the user's own rows, so the rows a foreign key cascade removes
	// with them are staged, read and reported too.
	seeds []resolvedEraser
	// owned, seeded and cascaded partition the staged rows: owned are the
	// user's, seeded are the ones a delete eraser matched, cascaded are the
	// rows staged because a foreign key named a staged row.
	owned, seeded, cascaded int
}

// staged is the number of rows staged in the table.
func (pt *erasePlanTable) staged() int { return pt.owned + pt.seeded + pt.cascaded }

type erasePlan struct {
	tables []*erasePlanTable // children first
	edges  []fkEdge          // cascade edges between plan tables, SET NULL excluded
	// blockers are the RESTRICT and NO ACTION edges whose parent table is in
	// the plan: a child row that is not itself staged refuses the erasure.
	blockers []fkEdge
	// cuts are the edges between erased rows that no delete order
	// satisfies; cutReferences empties them before the deletes.
	cuts []fkEdge
	// graph is every table the registry names, by lowercased name: the
	// battery plane reads the key, file columns and entity names of the
	// rows an eraser reaches through it.
	graph map[string]*dataTable
	// eraserGraph maps an eraser's name to the graph table it erases from:
	// the one the catalog resolves to the same table (graphIdentities).
	eraserGraph map[string]string
	// seededBy holds, per delete eraser whose matches seed the plan, the
	// rows its match staged. The battery plane adds them to the rows the
	// eraser's own delete finds left.
	seededBy map[string]int
}

// graphIdentities maps the identity (tableIdentity) of every graph table the
// database holds to its graph name. A graph table is an entity's or a
// pivot's, made by AutoMigrate's unquoted DDL, so its name resolves as the
// DDL folded it.
func graphIdentities(ctx context.Context, db rowQueryer, dialect migrate.Dialect, g *dataGraph) (map[string]string, error) {
	out := map[string]string{}
	for _, t := range g.sorted {
		id, err := tableIdentity(ctx, db, t, dialect, true)
		if err != nil {
			return nil, fmt.Errorf("framework: erase %w", err)
		}
		if _, held := out[id]; strings.HasPrefix(id, "table:") && !held {
			out[id] = t
		}
	}
	return out, nil
}

// planErase builds the erase plan before any transaction opens: the seed
// tables, every table the cascade reaches from them, and the column probes
// that make a missing column fail loud. Probes run against a.DB here because
// a single-connection pool (SQLite, MaxOpenConns(1)) would deadlock reading
// the catalog while the transaction holds the only connection.
//
// The seeds are the owner-scoped tables and the tables a delete eraser
// removes rows from. A battery-owned root such as the user table used to
// delete its rows on its own, and the rows the database cascade removed with
// them went without their stored objects or audit diffs.
func (a *App) planErase(ctx context.Context, dialect migrate.Dialect, erasers []resolvedEraser) (*erasePlan, error) {
	g, err := a.buildDataGraph()
	if err != nil {
		return nil, err
	}
	byTable := map[string]*erasePlanTable{}
	var queue []string
	for _, t := range g.sorted {
		if dt := g.tables[t]; dt.owner != "" {
			byTable[t] = &erasePlanTable{dt: dt, owner: dt.owner}
			queue = append(queue, t)
		}
	}
	// An eraser's table is matched to the graph by the table the catalog
	// resolves each to, never by spelling. users and public.users
	// (main.users on SQLite) are one table; on Postgres a quoted "Users" a
	// battery made is a table apart from the entity's users, and comparing
	// lowercased names seeded the entity's rows from the battery's eraser.
	// The eraser's name resolves as its own table is spelled, like the
	// battery plane's physicalTable.
	eraserGraph := map[string]string{}
	var byID map[string]string
	for _, e := range erasers {
		if byID == nil {
			if byID, err = graphIdentities(ctx, a.DB, dialect, g); err != nil {
				return nil, err
			}
		}
		id, err := tableIdentity(ctx, a.DB, e.eraser.Table, dialect, false)
		if err != nil {
			return nil, fmt.Errorf("framework: erase %w", err)
		}
		t, ok := byID[id]
		if !ok {
			continue // an absent table: the battery plane skips its eraser too
		}
		eraserGraph[e.eraser.Name] = t
		if e.eraser.Mode != datexport.EraseDelete {
			continue
		}
		// A pivot names no child, and a keyless table has nothing to
		// stage: the eraser deletes those rows itself.
		if dt := g.tables[t]; dt.pivot || len(dt.key) == 0 {
			continue
		}
		pt := byTable[t]
		if pt == nil {
			ok, err := tableExists(ctx, a.DB, t, dialect)
			if err != nil {
				return nil, fmt.Errorf("framework: erase probe %q: %w", t, err)
			}
			if !ok {
				continue // the battery plane skips the eraser too
			}
			pt = &erasePlanTable{dt: g.tables[t]}
			byTable[t] = pt
			queue = append(queue, t)
		}
		if err := requireColumns(ctx, a.DB, t, append([]string{e.eraser.Column}, whereColumns(e.eraser)...), dialect); err != nil {
			return nil, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
		}
		pt.seeds = append(pt.seeds, e)
	}
	absent := map[string]bool{}
	var edges []fkEdge
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, e := range g.edges {
			if e.parent != parent || e.setNull || e.blocks != "" || absent[e.child] {
				continue
			}
			child := g.tables[e.child]
			if child == nil || child.unmanaged {
				continue
			}
			pt := byTable[e.child]
			if pt == nil {
				ok, err := tableExists(ctx, a.DB, e.child, dialect)
				if err != nil {
					return nil, fmt.Errorf("framework: erase probe %q: %w", e.child, err)
				}
				if !ok {
					absent[e.child] = true // an absent table holds no rows to follow
					continue
				}
				pt = &erasePlanTable{dt: child}
				byTable[e.child] = pt
				queue = append(queue, e.child)
			}
			pt.reached = true
			edges = append(edges, e)
		}
	}

	// RESTRICT and NO ACTION edges into the plan: not followed, but checked
	// after staging. One whose child table is in the plan also orders the
	// delete, so the user's own child rows go before the parents they name.
	var blockers []fkEdge
	order := slices.Clone(edges)
	for _, e := range g.edges {
		if e.blocks == "" || byTable[e.parent] == nil || absent[e.child] {
			continue
		}
		child := g.tables[e.child]
		if child == nil || child.unmanaged {
			continue
		}
		if byTable[e.child] == nil {
			ok, err := tableExists(ctx, a.DB, e.child, dialect)
			if err != nil {
				return nil, fmt.Errorf("framework: erase probe %q: %w", e.child, err)
			}
			if !ok {
				absent[e.child] = true
				continue
			}
		}
		if err := requireColumns(ctx, a.DB, e.child, append(slices.Clone(child.key), e.cols...), dialect); err != nil {
			return nil, fmt.Errorf("framework: erase relation %q: %w", e.label, err)
		}
		blockers = append(blockers, e)
		if byTable[e.child] != nil {
			order = append(order, e)
		}
	}

	tables := slices.Sorted(maps.Keys(byTable))
	for _, t := range tables {
		pt := byTable[t]
		label := pt.dt.ownerName
		if label == "" {
			label = pt.dt.name
		}
		// The owner column first, so a table missing it reports that column.
		var cols []string
		if pt.owner != "" {
			cols = append(cols, pt.owner)
		}
		cols = append(cols, pt.dt.key...)
		for _, e := range edges {
			if e.child == t {
				cols = append(cols, e.cols...)
			}
		}
		if err := requireColumns(ctx, a.DB, t, cols, dialect); err != nil {
			return nil, fmt.Errorf("framework: erase entity %q: %w", label, err)
		}
	}
	plan := &erasePlan{edges: edges, blockers: blockers, graph: map[string]*dataTable{}, seededBy: map[string]int{}, eraserGraph: eraserGraph}
	for t, dt := range g.tables {
		plan.graph[strings.ToLower(t)] = dt
	}
	// A temp table hides an app table of the same name from every unqualified
	// reference after it, so no staging name may be a table the graph names.
	taken := map[string]bool{}
	for t := range g.tables {
		taken[strings.ToLower(t)] = true
	}
	// Tables with no key go first. Nothing can reference them, so deleting
	// them first never trips a constraint, and they are matched through
	// their foreign keys, which a parent deleted earlier could rewrite: a
	// SET NULL relation to it would empty the very column the match reads,
	// and the row would survive the count that named it.
	ordered := orderTables(tables, order, false)
	keyless := func(t string) bool { return len(byTable[t].dt.key) == 0 }
	ordered = append(slices.DeleteFunc(slices.Clone(ordered), func(t string) bool { return !keyless(t) }),
		slices.DeleteFunc(ordered, keyless)...)
	for i, t := range ordered {
		pt := byTable[t]
		if len(pt.dt.key) == 0 && pt.owner != "" {
			return nil, fmt.Errorf("framework: erase entity %q: an owner-scoped table needs a primary key", pt.label())
		}
		if len(pt.dt.key) > 0 {
			pt.tmp = fmt.Sprintf("gofastr_erase_%d", i)
			for taken[pt.tmp] {
				pt.tmp += "_"
			}
		}
		plan.tables = append(plan.tables, pt)
	}
	// A reference no delete order satisfies is cut before the deletes: one
	// that points at a table deleted earlier (only a cycle of tables puts
	// one there), and a RESTRICT self-reference, which refuses a parent's
	// delete even when the same statement deletes the child.
	pos := map[string]int{}
	for i, pt := range plan.tables {
		pos[pt.dt.table] = i
	}
	for _, e := range order {
		if len(byTable[e.child].dt.key) == 0 {
			continue
		}
		if e.child == e.parent && e.blocks == entity.OnDeleteRestrict || e.child != e.parent && pos[e.child] > pos[e.parent] {
			// Emptying a key column loses the key the delete matches
			// on: the row stayed and was reported erased.
			for _, c := range e.cols {
				if slices.Contains(byTable[e.child].dt.key, c) {
					return nil, fmt.Errorf("framework: erase relation %q: erased rows must be detached before the delete through key column %q, which cannot be emptied", e.label, c)
				}
			}
			plan.cuts = append(plan.cuts, e)
		}
	}
	return plan, nil
}

func (p *erasePlan) byTable() map[string]*erasePlanTable {
	m := make(map[string]*erasePlanTable, len(p.tables))
	for _, pt := range p.tables {
		m[pt.dt.table] = pt
	}
	return m
}

func (pt *erasePlanTable) label() string {
	if pt.dt.ownerName != "" {
		return pt.dt.ownerName
	}
	return pt.dt.name
}

// quotedCols renders cols as a select/insert list, each column prefixed by
// prefix (an alias, or "" for none).
func quotedCols(prefix string, cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = query.QuoteFolded(query.MustIdent(c))
		if prefix != "" {
			out[i] = prefix + "." + out[i]
		}
	}
	return strings.Join(out, ", ")
}

// colsEqual renders `l.lcols[0] = r.rcols[0] AND ...`. l and r are aliases
// or quoted table names.
func colsEqual(l string, lcols []string, r string, rcols []string) string {
	out := make([]string, len(lcols))
	for i := range lcols {
		out[i] = l + "." + query.QuoteFolded(query.MustIdent(lcols[i])) + " = " +
			r + "." + query.QuoteFolded(query.MustIdent(rcols[i]))
	}
	return strings.Join(out, " AND ")
}

// rowMatch is the predicate that selects pt's erased rows, with alias naming
// pt's table in the enclosing statement. A staged table matches its staging
// table. A table with no key (a raw Table may declare none) has none: nothing
// can reference it, so it is always a leaf of the plan, and its erased rows
// are those whose foreign key names a staged parent row.
func (p *erasePlan) rowMatch(pt *erasePlanTable, alias string) string {
	if pt.tmp != "" {
		return fmt.Sprintf("EXISTS (SELECT 1 FROM %s d WHERE %s)",
			query.QuoteFolded(query.MustIdent(pt.tmp)), colsEqual("d", pt.dt.key, alias, pt.dt.key))
	}
	by := p.byTable()
	var ors []string
	for _, e := range p.edges {
		if e.child != pt.dt.table {
			continue
		}
		ors = append(ors, fmt.Sprintf("EXISTS (SELECT 1 FROM %s d WHERE %s)",
			query.QuoteFolded(query.MustIdent(by[e.parent].tmp)), colsEqual("d", e.key, alias, e.cols)))
	}
	return "(" + strings.Join(ors, " OR ") + ")"
}

func execCount(ctx context.Context, tx *sql.Tx, stmt string, args ...any) (int, error) {
	res, err := tx.ExecContext(ctx, stmt, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// stage fills each plan table's temp table with the keys of the rows the
// erasure removes: the user's own rows, then, until a pass adds nothing,
// every row whose foreign key names a row already staged. The keys live in
// the database, so a user with a million rows binds no million parameters,
// and a self-reference or a cycle simply stops growing.
func (p *erasePlan) stage(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, userID string) error {
	for _, pt := range p.tables {
		if pt.tmp == "" {
			continue // keyless: matched through its parents (rowMatch)
		}
		qtmp := query.QuoteFolded(query.MustIdent(pt.tmp))
		qt := query.QuoteFolded(query.MustIdent(pt.dt.table))
		var stmts []string
		if dialect == migrate.DialectPostgres {
			// Dropped at commit or rollback, so a failed erasure leaves nothing behind.
			stmts = []string{fmt.Sprintf("CREATE TEMP TABLE %s ON COMMIT DROP AS SELECT %s FROM %s WITH NO DATA",
				qtmp, quotedCols("", pt.dt.key), qt)}
		} else {
			// SQLite keeps a temp table for the connection's life; a leftover
			// from an interrupted run is dropped first. temp. names the temp
			// schema only, never an app table of the same name.
			stmts = []string{
				"DROP TABLE IF EXISTS temp." + qtmp,
				fmt.Sprintf("CREATE TEMP TABLE %s AS SELECT %s FROM %s LIMIT 0", qtmp, quotedCols("", pt.dt.key), qt),
			}
		}
		for _, s := range stmts {
			if _, err := tx.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("framework: erase entity %q: stage: %w", pt.label(), err)
			}
		}
	}
	for _, pt := range p.tables {
		if pt.owner == "" {
			continue
		}
		stmt := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s t WHERE t.%s = $1",
			query.QuoteFolded(query.MustIdent(pt.tmp)), quotedCols("", pt.dt.key), quotedCols("t", pt.dt.key),
			query.QuoteFolded(query.MustIdent(pt.dt.table)), query.QuoteFolded(query.MustIdent(pt.owner)))
		n, err := execCount(ctx, tx, stmt, userID)
		if err != nil {
			return fmt.Errorf("framework: erase entity %q: %w", pt.label(), err)
		}
		pt.owned = n
	}
	// A delete eraser's matches, each row once: a row the user owns, or one
	// an earlier eraser matched, counts where it was first staged. A row
	// with a NULL in its key is left to the eraser's own delete, which runs
	// after the plan's: no foreign key can name it, so nothing cascades from
	// it, and staging it would count a row the staged delete cannot match.
	for _, pt := range p.tables {
		for _, e := range pt.seeds {
			qtmp := query.QuoteFolded(query.MustIdent(pt.tmp))
			match, args := eraserMatch(e, 1)
			stmt := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s t WHERE %s AND NOT %s AND NOT EXISTS (SELECT 1 FROM %s x WHERE %s)",
				qtmp, quotedCols("", pt.dt.key), quotedCols("t", pt.dt.key),
				query.QuoteFolded(query.MustIdent(pt.dt.table)), match, anyNull("t", pt.dt.key),
				qtmp, colsEqual("x", pt.dt.key, "t", pt.dt.key))
			n, err := execCount(ctx, tx, stmt, args...)
			if err != nil {
				return fmt.Errorf("framework: erase %q: stage: %w", e.eraser.Name, err)
			}
			pt.seeded += n
			p.seededBy[e.eraser.Name] = n
		}
	}
	by := p.byTable()
	for {
		added := 0
		for _, e := range p.edges {
			c, par := by[e.child], by[e.parent]
			if c.tmp == "" {
				continue
			}
			qtmp := query.QuoteFolded(query.MustIdent(c.tmp))
			// A NULL key never equals the staged copy of itself, so such a
			// row would be added again on every pass; checkNullKeys refuses it.
			stmt := fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s c WHERE NOT %s AND EXISTS (SELECT 1 FROM %s p WHERE %s) AND NOT EXISTS (SELECT 1 FROM %s x WHERE %s)",
				qtmp, quotedCols("", c.dt.key), quotedCols("c", c.dt.key),
				query.QuoteFolded(query.MustIdent(c.dt.table)), anyNull("c", c.dt.key),
				query.QuoteFolded(query.MustIdent(par.tmp)), colsEqual("p", e.key, "c", e.cols),
				qtmp, colsEqual("x", c.dt.key, "c", c.dt.key))
			n, err := execCount(ctx, tx, stmt)
			if err != nil {
				return fmt.Errorf("framework: erase entity %q: follow %s: %w", c.label(), e.label, err)
			}
			c.cascaded += n
			added += n
		}
		if added == 0 {
			break
		}
	}
	if err := p.checkNullKeys(ctx, tx, userID); err != nil {
		return err
	}
	for _, pt := range p.tables {
		if pt.tmp != "" {
			continue
		}
		stmt := fmt.Sprintf("SELECT COUNT(*) FROM %s t WHERE %s",
			query.QuoteFolded(query.MustIdent(pt.dt.table)), p.rowMatch(pt, "t"))
		if err := tx.QueryRowContext(ctx, stmt).Scan(&pt.cascaded); err != nil {
			return fmt.Errorf("framework: erase entity %q: count: %w", pt.label(), err)
		}
	}
	return p.checkBlockers(ctx, tx)
}

// checkBlockers refuses the erasure when a row the plan does not erase
// names a staged row through a RESTRICT or NO ACTION relation. The developer
// declared that such a row blocks its parent's delete, and the erasure does
// not delete it to get around that. It runs after staging and before any
// delete, so a refused erasure (or dry run) changes nothing.
func (p *erasePlan) checkBlockers(ctx context.Context, tx *sql.Tx) error {
	by := p.byTable()
	for _, e := range p.blockers {
		par := by[e.parent]
		stmt := fmt.Sprintf("SELECT COUNT(*) FROM %s c WHERE EXISTS (SELECT 1 FROM %s p WHERE %s)",
			query.QuoteFolded(query.MustIdent(e.child)),
			query.QuoteFolded(query.MustIdent(par.tmp)), colsEqual("p", e.key, "c", e.cols))
		if c := by[e.child]; c != nil {
			stmt += " AND NOT " + p.rowMatch(c, "c")
		}
		var n int
		if err := tx.QueryRowContext(ctx, stmt).Scan(&n); err != nil {
			return fmt.Errorf("framework: erase relation %q: check ON DELETE %s: %w", e.label, e.blocks, err)
		}
		if n > 0 {
			return fmt.Errorf("framework: erase refused: relation %q declares ON DELETE %s and %d row(s) in %q that this erasure does not remove reference rows it would; nothing was erased",
				e.label, e.blocks, n, e.child)
		}
	}
	return nil
}

// anyNull renders `(alias.c1 IS NULL OR ...)` over cols.
func anyNull(alias string, cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = alias + "." + query.QuoteFolded(query.MustIdent(c)) + " IS NULL"
	}
	return "(" + strings.Join(out, " OR ") + ")"
}

// checkNullKeys refuses the erasure when a row it would remove has a NULL
// in its primary key, which SQLite allows for a key that is not an INTEGER.
// The staging table cannot hold such a row's identity (NULL equals nothing,
// itself included), so the delete would miss it while the report counted
// it, and the parent's delete would then trip its foreign key.
func (p *erasePlan) checkNullKeys(ctx context.Context, tx *sql.Tx, userID string) error {
	by := p.byTable()
	for _, pt := range p.tables {
		if pt.tmp == "" {
			continue
		}
		var ors []string
		var args []any
		if pt.owner != "" {
			ors = append(ors, "c."+query.QuoteFolded(query.MustIdent(pt.owner))+" = $1")
			args = append(args, userID)
		}
		for _, e := range p.edges {
			if e.child == pt.dt.table {
				ors = append(ors, fmt.Sprintf("EXISTS (SELECT 1 FROM %s p WHERE %s)",
					query.QuoteFolded(query.MustIdent(by[e.parent].tmp)), colsEqual("p", e.key, "c", e.cols)))
			}
		}
		if len(ors) == 0 {
			continue
		}
		var n int
		stmt := fmt.Sprintf("SELECT COUNT(*) FROM %s c WHERE %s AND (%s)",
			query.QuoteFolded(query.MustIdent(pt.dt.table)), anyNull("c", pt.dt.key), strings.Join(ors, " OR "))
		if err := tx.QueryRowContext(ctx, stmt, args...).Scan(&n); err != nil {
			return fmt.Errorf("framework: erase entity %q: check keys: %w", pt.label(), err)
		}
		if n > 0 {
			return fmt.Errorf("framework: erase refused: %d row(s) in %q that this erasure would remove have a NULL primary key, so they cannot be told apart; give them a key; nothing was erased",
				n, pt.dt.table)
		}
	}
	return nil
}

// cutReferences empties the cut edges' columns on erased rows that name
// another erased row, so the deletes that follow meet no reference into a
// row already gone. A column declared NOT NULL cannot be emptied, and the
// erasure (dry run included) fails naming the relation.
func (p *erasePlan) cutReferences(ctx context.Context, tx *sql.Tx) error {
	by := p.byTable()
	for _, e := range p.cuts {
		c := by[e.child]
		sets := make([]string, len(e.cols))
		for i, col := range e.cols {
			sets[i] = query.QuoteFolded(query.MustIdent(col)) + " = NULL"
		}
		stmt := fmt.Sprintf("UPDATE %s AS t SET %s WHERE %s AND EXISTS (SELECT 1 FROM %s p WHERE %s)",
			query.QuoteFolded(query.MustIdent(e.child)), strings.Join(sets, ", "), p.rowMatch(c, "t"),
			query.QuoteFolded(query.MustIdent(by[e.parent].tmp)), colsEqual("p", e.key, "t", e.cols))
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("framework: erase relation %q: erased rows reference each other in a cycle and the reference cannot be emptied before the delete: %w", e.label, err)
		}
	}
	return nil
}

// deleteStaged deletes every staged row, children first, after cutting the
// references no order satisfies. A self-reference is one statement, which
// both engines check at the statement's end. The target
// carries the alias t: correlating on its bare name let a table named d
// resolve to the staging alias inside the subquery, so every row matched.
func (p *erasePlan) deleteStaged(ctx context.Context, tx *sql.Tx) error {
	if err := p.cutReferences(ctx, tx); err != nil {
		return err
	}
	for _, pt := range p.tables {
		stmt := fmt.Sprintf("DELETE FROM %s AS t WHERE %s",
			query.QuoteFolded(query.MustIdent(pt.dt.table)), p.rowMatch(pt, "t"))
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("framework: erase entity %q: %w", pt.label(), err)
		}
	}
	return nil
}

// dropStaged drops the temp tables. Postgres would drop them at commit
// anyway; SQLite keeps them for the connection's life.
func (p *erasePlan) dropStaged(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect) error {
	schemaName := "temp."
	if dialect == migrate.DialectPostgres {
		schemaName = "pg_temp."
	}
	for _, pt := range p.tables {
		if pt.tmp == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS "+schemaName+query.QuoteFolded(query.MustIdent(pt.tmp))); err != nil {
			return fmt.Errorf("framework: erase: drop staging table: %w", err)
		}
	}
	return nil
}

// report adds the plan's counts: the user's rows per owner-scoped table,
// then the cascaded rows per reached table.
func (p *erasePlan) report(rep *EraseReport) {
	for _, pt := range p.tables {
		if pt.owner != "" {
			rep.Entities = append(rep.Entities, EraseTableResult{
				Name: pt.dt.ownerName, Source: "entity", Table: pt.dt.table, Mode: "delete", RowsAffected: pt.owned,
			})
		}
	}
	for _, pt := range p.tables {
		if pt.reached {
			rep.Cascaded = append(rep.Cascaded, EraseTableResult{
				Name: pt.dt.name, Source: "cascade", Table: pt.dt.table, Mode: "delete", RowsAffected: pt.cascaded,
			})
		}
	}
}

// keyString is the record id the framework spells for one row's key values,
// the same spelling an audit row's record_id carries (entity.RowKey).
func keyString(vals []any) (string, bool) {
	parts := make([]string, len(vals))
	for i, v := range vals {
		if v == nil {
			return "", false
		}
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		parts[i] = fmt.Sprint(v)
	}
	return entity.JoinKey(parts), true
}

// auditRecordSet is the record ids of one table's erased rows, under every
// entity name stored in the table.
type auditRecordSet struct {
	entities []string
	ids      []string
}

// auditRecords reads the record ids of the staged rows. Each id is listed in
// the spelling an audit row stores it in: as is, and as the sha256 digest
// WithAuditLog stores in place of a sensitive key (AuditConfig.keyID). An id
// a host's Redact callback remapped on delete cannot be derived and is not
// matched.
func (p *erasePlan) auditRecords(ctx context.Context, tx *sql.Tx) ([]auditRecordSet, error) {
	var out []auditRecordSet
	for _, pt := range p.tables {
		if len(pt.dt.entities) == 0 || pt.tmp == "" || pt.staged() == 0 {
			continue
		}
		ids, err := readRecordIDs(ctx, tx, fmt.Sprintf("SELECT %s FROM %s",
			quotedCols("", pt.dt.key), query.QuoteFolded(query.MustIdent(pt.tmp))), nil, len(pt.dt.key))
		if err != nil {
			return nil, fmt.Errorf("framework: erase entity %q: read keys: %w", pt.label(), err)
		}
		out = append(out, auditRecordSet{entities: pt.dt.entities, ids: ids})
	}
	return out, nil
}

// readRecordIDs runs stmt, which selects one row key of width columns per
// row, and returns each key as the record ids an audit row may store.
func readRecordIDs(ctx context.Context, tx *sql.Tx, stmt string, args []any, width int) ([]string, error) {
	rows, err := tx.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	vals := make([]any, width)
	dest := make([]any, width)
	for i := range vals {
		dest[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		id, ok := keyString(vals)
		if !ok {
			continue
		}
		// keyID strips control bytes before storing or hashing.
		id = sanitizeAuditField(id)
		sum := sha256.Sum256([]byte(id))
		ids = append(ids, id, "sha256:"+hex.EncodeToString(sum[:]))
	}
	return ids, rows.Err()
}

// objectKeys collects storage keys, each once, in the order first named.
type objectKeys struct {
	seen map[string]bool
	keys []string
}

func (o *objectKeys) add(k string) {
	k = strings.TrimSpace(k)
	if k == "" || o.seen[k] {
		return
	}
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	o.seen[k] = true
	o.keys = append(o.keys, k)
}

// lockRead is the suffix that locks the rows a read inside the erasure
// selects. On Postgres a plain read leaves them free: under READ COMMITTED
// another request could swap the avatar between the read and the delete,
// and the row went with an object nobody had read the key of. SQLite's
// single writer keeps the rows as read until the erasure commits.
func lockRead(dialect migrate.Dialect) string {
	if dialect == migrate.DialectPostgres {
		return " FOR UPDATE"
	}
	return ""
}

// storedObjectKeys reads every storage key referenced by the staged rows:
// the schema.Image / schema.File columns themselves, plus the storage_ref of
// each rendition in the sibling "<field>_variants" JSON column. A cascaded
// row's files go with it, as nothing names them once it is gone.
//
// It runs inside the transaction, after staging and before the delete: once
// the rows are gone nothing names the objects, and the erasure would report
// success while the user's avatar stayed downloadable at /uploads. The rows
// read are locked until the erasure commits (see lockRead).
func (a *App) storedObjectKeys(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, p *erasePlan, into *objectKeys) error {
	for _, pt := range p.tables {
		cols := append(append([]string{}, pt.dt.fileCols...), pt.dt.variantCols...)
		if len(cols) == 0 || pt.staged() == 0 {
			continue
		}
		stmt := fmt.Sprintf("SELECT %s FROM %s t WHERE %s%s",
			quotedCols("t", cols), query.QuoteFolded(query.MustIdent(pt.dt.table)), p.rowMatch(pt, "t"), lockRead(dialect))
		if err := readObjectKeys(ctx, tx, stmt, nil, cols, pt.dt.variantCols, into); err != nil {
			return fmt.Errorf("framework: erase %q: read file columns: %w", pt.label(), err)
		}
	}
	return nil
}

// readObjectKeys runs stmt, which selects cols, and adds every key a file
// column holds and every rendition a variants column lists.
func readObjectKeys(ctx context.Context, tx *sql.Tx, stmt string, args []any, cols, variantCols []string, into *objectKeys) error {
	rows, err := tx.QueryContext(ctx, stmt, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	vals := make([]sql.NullString, len(cols))
	dest := make([]any, len(cols))
	for i := range vals {
		dest[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		for i, col := range cols {
			if !vals[i].Valid {
				continue
			}
			if slices.Contains(variantCols, col) {
				// A column that does not parse is not a rendition
				// list; leaving it alone is better than guessing.
				// The CRUD write path parses with the same function.
				refs, _ := file.VariantStorageRefs([]byte(vals[i].String))
				for _, ref := range refs {
					into.add(ref)
				}
				continue
			}
			into.add(vals[i].String)
		}
	}
	return rows.Err()
}

// auditShape is what the audit plane may touch in the audit table. actor_id
// is required. The diff scrub needs a diff column, and the scrub of rows that
// record an erased row needs entity and record_id too: a custom audit table
// (WithEraseAuditTable) without them has nothing of that kind to scrub.
type auditShape struct {
	table   string
	diff    bool // scrub the user's own rows' diff
	records bool // scrub the diff of rows recording an erased row
}

// quoted names the audit table as EnsureAuditTable created it: folded,
// like AutoMigrate's entity tables.
func (at auditShape) quoted() string { return query.QuoteFolded(query.MustIdent(at.table)) }

// auditRecordChunk bounds the record ids bound into one statement, well
// under either engine's parameter limit.
const auditRecordChunk = 500

// recordChunks calls fn with each entity name and each chunk of ids, as the
// `entity = $n AND record_id IN (...)` clause and its arguments, numbered
// from first.
func recordChunks(recs []auditRecordSet, first int, fn func(clause string, args []any) error) error {
	for _, rec := range recs {
		for _, ent := range rec.entities {
			for ids := range slices.Chunk(rec.ids, auditRecordChunk) {
				marks := make([]string, len(ids))
				args := []any{ent}
				for i, id := range ids {
					marks[i] = fmt.Sprintf("$%d", first+1+i)
					args = append(args, id)
				}
				clause := fmt.Sprintf("entity = $%d AND record_id IN (%s)", first, strings.Join(marks, ", "))
				if err := fn(clause, args); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// eraseAuditPlane anonymizes the user's actor_id and replaces the diff of
// the user's rows and of the rows recording an erased row with
// erasedAuditDiff (decision E2). A NULL diff stays NULL: there was nothing
// to scrub. It returns the rows changed, each counted once.
func eraseAuditPlane(ctx context.Context, tx *sql.Tx, at auditShape, userID, tombstone string, recs []auditRecordSet) (int, error) {
	qt := at.quoted()
	// The diff first, while actor_id still names the user's rows.
	if at.diff {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET diff = $1 WHERE actor_id = $2 AND diff IS NOT NULL", qt),
			erasedAuditDiff, userID); err != nil {
			return 0, err
		}
	}
	n, err := eraseAnonymizeRaw(ctx, tx, qt, "actor_id", userID, tombstone, "actor_id")
	if err != nil || !at.records {
		return n, err
	}
	// A row already carrying the marker (the user's own, just above, or one
	// a previous erasure scrubbed) is not counted again.
	err = recordChunks(recs, 2, func(clause string, args []any) error {
		c, err := execCount(ctx, tx, fmt.Sprintf("UPDATE %s SET diff = $1 WHERE diff IS NOT NULL AND diff <> $1 AND %s", qt, clause),
			append([]any{erasedAuditDiff}, args...)...)
		n += c
		return err
	})
	return n, err
}

// eraseWrite performs the erasure inside one transaction. Existence probes
// run against a.DB BEFORE BeginTx so a single-connection SQLite pool does
// not deadlock (the tx would otherwise hold the only connection while the
// probe waited for one). Erasers arrive already identity-resolved (see
// resolveEraserIdentities): each carries the value its match column is bound
// against, the user id, or a resolved identity like email.
//
// A dry run is the same run with the transaction rolled back and no stored
// object deleted. Counting each plane on its own disagreed with the erasure
// wherever one write changes what the next one matches: a row two delete
// erasers both reach, a constraint only the delete trips.
func (a *App) eraseWrite(ctx context.Context, dialect migrate.Dialect, userID string, cfg eraseConfig, plan *erasePlan, erasers []resolvedEraser, dryRun bool) (EraseReport, error) {
	report := EraseReport{DryRun: dryRun}

	liveErasers := make([]resolvedEraser, 0, len(erasers))
	for _, e := range erasers {
		// A battery table is spelled as its DDL made it: folding a quoted
		// "Jobs" erased from another table, jobs.
		phys, exists, err := physicalTable(ctx, a.DB, e.eraser.Table, dialect, false)
		if err != nil {
			return report, fmt.Errorf("framework: erase probe %q: %w", e.eraser.Table, err)
		}
		if !exists {
			if !dryRun {
				fmt.Fprintf(os.Stderr, "framework: erase: table %q absent, skipping\n", e.eraser.Table)
			}
			continue
		}
		e.table = phys
		liveErasers = append(liveErasers, e)
	}
	audit, err := a.probeAuditTable(ctx, dialect, cfg)
	if err != nil {
		return report, err
	}
	// Column probes run BEFORE BeginTx: a single-connection pool (SQLite in
	// tests, MaxOpenConns(1) in production) would deadlock reading schema
	// while the transaction holds the only connection. The entity plane's
	// probes ran in planErase.
	for _, e := range liveErasers {
		for _, col := range append([]string{e.eraser.Column}, whereColumns(e.eraser)...) {
			if err := requireColumn(ctx, a.DB, e.table, col, dialect); err != nil {
				return report, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
			}
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

	// Entity plane: stage the user's rows and everything the cascade reaches,
	// read what the rows name (stored objects, audit record ids), then
	// hard-delete children first (no soft-delete filter).
	if err := plan.stage(ctx, tx, dialect, userID); err != nil {
		return report, err
	}
	// Keys are read only for a real run with somewhere to delete them from.
	readKeys := !dryRun && a.Storage != nil
	var stored objectKeys
	if readKeys {
		if err := a.storedObjectKeys(ctx, tx, dialect, plan, &stored); err != nil {
			return report, err
		}
		if eraseKeysRead != nil {
			eraseKeysRead()
		}
	}
	var recs []auditRecordSet
	if audit != nil && audit.records {
		if recs, err = plan.auditRecords(ctx, tx); err != nil {
			return report, err
		}
	}
	if err := plan.deleteStaged(ctx, tx); err != nil {
		return report, err
	}
	if err := plan.dropStaged(ctx, tx, dialect); err != nil {
		return report, err
	}
	plan.report(&report)

	// Battery plane: registered erasers, each matched against its resolved
	// value (user id, or a resolved identity like email).
	var batteryKeys *objectKeys
	if readKeys {
		batteryKeys = &stored
	}
	batteryRecs, results, err := plan.eraseBatteries(ctx, tx, dialect, liveErasers, audit != nil && audit.records, batteryKeys)
	if err != nil {
		return report, err
	}
	recs = append(recs, batteryRecs...)
	report.Batteries = append(report.Batteries, results...)
	storedKeys := stored.keys

	// Audit plane: anonymize actor_id and scrub diff (built-in, framework-owned table).
	if audit != nil {
		n, aerr := eraseAuditPlane(ctx, tx, *audit, userID, cfg.auditTombstone, recs)
		if aerr != nil {
			return report, fmt.Errorf("framework: erase audit: %w", aerr)
		}
		report.Audit = &EraseTableResult{
			Name: cfg.auditTable, Source: "audit", Table: cfg.auditTable,
			Mode: "anonymize", RowsAffected: n,
		}
	}

	if dryRun {
		// Every write is undone before the stores count what they would
		// remove: a store eraser that reads the database would otherwise
		// wait on the connection the transaction holds.
		committed = true // the transaction is finished; the deferred rollback has nothing to do
		if err := tx.Rollback(); err != nil {
			return report, fmt.Errorf("framework: erase dry run: rollback: %w", err)
		}
		return report, a.eraseStores(ctx, userID, true, &report)
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("framework: erase: commit: %w", err)
	}
	committed = true

	// Objects last: the rows are gone, so nothing points at these bytes any
	// more. Doing it before the commit would delete a user's files and then
	// leave the rows behind if the transaction rolled back, which is the
	// worse of the two failures.
	//
	// Only keys erasure can attribute to this user are deleted: never an
	// external URL, and never a key a surviving row still names (see
	// erase_shared_keys.go). If that check fails, nothing is deleted.
	var objErr error
	if deletable, err := a.unsharedObjectKeys(ctx, dialect, storedKeys); err != nil {
		objErr = fmt.Errorf("framework: erase: rows deleted but no stored objects were removed: checking which keys other rows still name: %w", err)
	} else {
		objErr = a.eraseStoredObjects(ctx, deletable)
	}
	// Stores outside the database last, for the reason objects are: a
	// failure here cannot roll the rows back, and the reverse order would
	// leave a store erased behind rows that survived.
	return report, errors.Join(objErr, a.eraseStores(ctx, userID, false, &report))
}

// eraseStores runs every registered datexport.StoreEraser, producers
// first (AllStoreErasers' order), and adds the ones that succeed to the
// report. Every one runs even when another fails,
// so one unreachable store does not keep the user's data in the others.
// The SQL part is committed by now (or rolled back, on a dry run), so a
// failure is reported with the store's name, never folded into success:
// the operator erases again once the store is reachable, and each eraser
// finds only what is left.
func (a *App) eraseStores(ctx context.Context, userID string, dryRun bool, rep *EraseReport) error {
	var failed []string
	for _, s := range datexport.AllStoreErasers() {
		n, err := runStoreEraser(ctx, s, userID, dryRun)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s (%v)", s.Name, err))
			continue
		}
		rep.Stores = append(rep.Stores, EraseTableResult{Name: s.Name, Source: s.Source, Mode: "delete", RowsAffected: n})
	}
	if len(failed) == 0 {
		return nil
	}
	if dryRun {
		return fmt.Errorf("framework: erase dry run: %d store(s) could not be counted: %s", len(failed), strings.Join(failed, "; "))
	}
	return fmt.Errorf("framework: erase: rows deleted but %d store(s) failed and may still hold the user's data: %s",
		len(failed), strings.Join(failed, "; "))
}

// runStoreEraser calls a store's Erase, a host or battery callback, with a
// recover guard: a panic in one store is that store's error.
func runStoreEraser(ctx context.Context, s datexport.StoreEraser, userID string, dryRun bool) (n int, err error) {
	defer func() {
		if v := recover(); v != nil {
			n, err = 0, fmt.Errorf("panicked: %s", textsafe.Recovered(v))
		}
	}()
	return s.Erase(ctx, userID, dryRun)
}

// probeAuditTable returns the audit table's shape, or nil when it is absent.
// actor_id is required; diff, entity and record_id decide which scrubs run.
func (a *App) probeAuditTable(ctx context.Context, dialect migrate.Dialect, cfg eraseConfig) (*auditShape, error) {
	exists, err := tableExists(ctx, a.DB, cfg.auditTable, dialect)
	if err != nil {
		return nil, fmt.Errorf("framework: erase probe audit: %w", err)
	}
	if !exists {
		return nil, nil
	}
	live, err := migrate.ReadLiveColumns(ctx, a.DB, cfg.auditTable, dialect)
	if err != nil {
		return nil, fmt.Errorf("framework: erase probe audit: %w", err)
	}
	has := func(col string) bool {
		for name := range live {
			if strings.EqualFold(name, col) {
				return true
			}
		}
		return false
	}
	if len(live) > 0 && !has("actor_id") {
		return nil, fmt.Errorf("framework: erase audit: table %q has no column %q: erasure would silently match nothing", cfg.auditTable, "actor_id")
	}
	at := &auditShape{table: cfg.auditTable, diff: has("diff")}
	at.records = at.diff && has("entity") && has("record_id")
	return at, nil
}

// eraseStoredObjects deletes the collected objects from App.Storage. A key
// that is already gone is not an error (erasure is idempotent, and a
// half-finished earlier run is the normal way to get here). Anything else is
// reported: the rows are committed at this point, so a silent failure would
// leave the user's files reachable behind a report that says otherwise, and
// the operator needs the key names to finish the job.
func (a *App) eraseStoredObjects(ctx context.Context, keys []string) error {
	if a.Storage == nil || len(keys) == 0 {
		return nil
	}
	var failed []string
	for _, k := range keys {
		if err := a.Storage.Delete(ctx, k); err != nil && !errors.Is(err, os.ErrNotExist) {
			failed = append(failed, fmt.Sprintf("%s (%v)", k, err))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("framework: erase: rows deleted but %d stored object(s) survive and remain reachable: %s",
			len(failed), strings.Join(failed, "; "))
	}
	return nil
}

// eraseDelete executes DELETE FROM table WHERE col = userID inside tx and
// returns the number of rows deleted. Identifiers are MustIdent-guarded; userID
// is a bound argument.
func eraseDelete(ctx context.Context, tx *sql.Tx, e resolvedEraser) (int, error) {
	match, args := eraserMatch(e, 1)
	return execCount(ctx, tx, fmt.Sprintf("DELETE FROM %s WHERE %s", quotePhysical(e.table), match), args...)
}

// eraseRemoveMember takes the bound value out of the JSON string array in
// the eraser's column, on every row that lists it and meets its Where
// conditions. The rest of the array keeps its order; an array left with
// no members is written as []. The value is a bound argument, $1 in
// every position.
//
// With a MemberSeparator, a member is compared by the part after its
// first separator (the whole member when it holds none), so a qualified
// member such as a feature flag's "acme:42" is removed with the bare
// "42". The separator is bound too, as $2.
func eraseRemoveMember(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, e resolvedEraser) (int, error) {
	qt := quotePhysical(e.table)
	qc := query.QuoteFolded(query.MustIdent(e.eraser.Column))
	args := []any{e.value}
	member := func(v string) string { return v }
	if sep := e.eraser.MemberSeparator; sep != "" {
		args = append(args, sep)
		find, width := "instr(%s, $2)", "length($2)"
		if dialect == migrate.DialectPostgres {
			find, width = "strpos(%s, $2::text)", "length($2::text)"
		}
		member = func(v string) string {
			at := fmt.Sprintf(find, v)
			return fmt.Sprintf("(CASE WHEN %[2]s > 0 THEN substr(%[1]s, %[2]s + %[3]s) ELSE %[1]s END)", v, at, width)
		}
	}
	var rest, holds string
	if dialect == migrate.DialectPostgres {
		rest = fmt.Sprintf("COALESCE((SELECT json_agg(m.v ORDER BY m.n)::text FROM json_array_elements_text(%s::json) WITH ORDINALITY AS m(v, n) WHERE %s <> $1), '[]')", qc, member("m.v"))
		holds = fmt.Sprintf("EXISTS (SELECT 1 FROM json_array_elements_text(%s::json) AS h(v) WHERE %s = $1)", qc, member("h.v"))
	} else {
		rest = fmt.Sprintf("(SELECT json_group_array(m.value) FROM (SELECT value FROM json_each(%s) WHERE %s <> $1 ORDER BY key) AS m)", qc, member("value"))
		holds = fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE %s = $1)", qc, member("value"))
	}
	// The match column is the array itself, so only the Where conditions
	// narrow the rows; their placeholders follow the value's (and the
	// separator's).
	conds, wargs := whereConds(e.eraser, len(args)+1)
	if conds != "" {
		holds += " AND " + conds
	}
	return execCount(ctx, tx, fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s", qt, qc, rest, holds), append(args, wargs...)...)
}

// eraserMatch is the condition an eraser's rows meet: its match column
// equals the bound value and every Where column its value, ANDed, with
// placeholders numbered from first. The arguments come back in
// placeholder order.
func eraserMatch(e resolvedEraser, first int) (string, []any) {
	match := fmt.Sprintf("%s = $%d", query.QuoteFolded(query.MustIdent(e.eraser.Column)), first)
	conds, args := whereConds(e.eraser, first+1)
	if conds != "" {
		match += " AND " + conds
	}
	return match, append([]any{e.value}, args...)
}

// whereConds is an eraser's Where conditions ANDed, placeholders numbered
// from first, with their values in placeholder order. No conditions is "".
func whereConds(e datexport.DataEraser, first int) (string, []any) {
	conds := make([]string, len(e.Where))
	args := make([]any, len(e.Where))
	for i, w := range e.Where {
		conds[i] = fmt.Sprintf("%s = $%d", query.QuoteFolded(query.MustIdent(w.Column)), first+i)
		args[i] = w.Value
	}
	return strings.Join(conds, " AND "), args
}

// whereColumns lists the columns of an eraser's Where conditions.
func whereColumns(e datexport.DataEraser) []string {
	cols := make([]string, len(e.Where))
	for i, w := range e.Where {
		cols[i] = w.Column
	}
	return cols
}

// eraseKeysRead is a test seam: when non-nil it runs after the erasure has
// read the staged rows' stored-object keys and before it deletes the rows,
// the window a concurrent update of those rows lives in. Nothing installs
// it outside the tests.
var eraseKeysRead func()

// eraseBatteries runs the registered erasers. Each eraser's rows are read
// before any of them is written, because every write changes what the
// next eraser matches: an anonymizer tombstones its match column, and a
// second anonymizer or a delete on that column then matched nothing. A row
// on an entity table gives up its record ids for the audit plane (when
// wantRecs) and the storage keys of the file columns the eraser removes,
// every one for a delete and the scrubbed ones for an anonymizer (when keys
// is set). Deletes then run first, and every anonymizer on a table runs as
// one UPDATE (eraseAnonymizeTable). Results come back in eraser order.
func (p *erasePlan) eraseBatteries(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, erasers []resolvedEraser, wantRecs bool, keys *objectKeys) ([]auditRecordSet, []EraseTableResult, error) {
	var recs []auditRecordSet
	var anonTables []string
	anon := map[string][]int{} // physical table → indexes of its anonymizers
	for i, e := range erasers {
		if e.eraser.Mode == datexport.EraseAnonymize {
			if len(e.eraser.ScrubColumns) == 0 {
				return nil, nil, fmt.Errorf("framework: erase %q: EraseAnonymize requires ScrubColumns", e.eraser.Name)
			}
			t := e.table
			if anon[t] == nil {
				anonTables = append(anonTables, t)
			}
			anon[t] = append(anon[t], i)
		}
		dt := p.graph[strings.ToLower(p.eraserGraph[e.eraser.Name])]
		if dt == nil || dt.pivot || e.eraser.Mode == datexport.EraseRemoveMember {
			continue
		}
		qt := quotePhysical(e.table)
		match, margs := eraserMatch(e, 1)
		if wantRecs && len(dt.entities) > 0 && len(dt.key) > 0 {
			ids, err := readRecordIDs(ctx, tx, fmt.Sprintf("SELECT %s FROM %s WHERE %s", quotedCols("", dt.key), qt, match),
				margs, len(dt.key))
			if err != nil {
				return nil, nil, fmt.Errorf("framework: erase %q: read keys: %w", e.eraser.Name, err)
			}
			recs = append(recs, auditRecordSet{entities: dt.entities, ids: ids})
		}
		if keys == nil {
			continue
		}
		var cols []string
		for _, c := range append(slices.Clone(dt.fileCols), dt.variantCols...) {
			if e.eraser.Mode == datexport.EraseDelete || slices.ContainsFunc(e.eraser.ScrubColumns, func(s string) bool { return strings.EqualFold(s, c) }) {
				cols = append(cols, c)
			}
		}
		if len(cols) == 0 {
			continue
		}
		stmt := fmt.Sprintf("SELECT %s FROM %s WHERE %s%s", quotedCols("", cols), qt, match, lockRead(dialect))
		if err := readObjectKeys(ctx, tx, stmt, margs, cols, dt.variantCols, keys); err != nil {
			return nil, nil, fmt.Errorf("framework: erase %q: read file columns: %w", e.eraser.Name, err)
		}
	}

	results := make([]EraseTableResult, len(erasers))
	for i, e := range erasers {
		if e.eraser.Mode != datexport.EraseDelete {
			continue
		}
		// A seeded eraser's staged rows went with the plan's delete; its
		// own delete finds only what the plan did not stage.
		n, err := eraseDelete(ctx, tx, e)
		if err != nil {
			return nil, nil, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
		}
		n += p.seededBy[e.eraser.Name]
		results[i] = EraseTableResult{Name: e.eraser.Name, Source: e.eraser.Source, Table: e.eraser.Table, Mode: "delete", RowsAffected: n}
	}
	for i, e := range erasers {
		if e.eraser.Mode != datexport.EraseRemoveMember {
			continue
		}
		n, err := eraseRemoveMember(ctx, tx, dialect, e)
		if err != nil {
			return nil, nil, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
		}
		results[i] = EraseTableResult{Name: e.eraser.Name, Source: e.eraser.Source, Table: e.eraser.Table, Mode: "remove", RowsAffected: n}
	}
	for _, t := range anonTables {
		group := make([]resolvedEraser, len(anon[t]))
		for j, i := range anon[t] {
			group[j] = erasers[i]
		}
		counts, err := eraseAnonymizeTable(ctx, tx, group)
		if err != nil {
			return nil, nil, err
		}
		for j, i := range anon[t] {
			e := erasers[i].eraser
			results[i] = EraseTableResult{Name: e.Name, Source: e.Source, Table: e.Table, Mode: "anonymize", RowsAffected: counts[j]}
		}
	}
	// An eraser of another mode does nothing and reports nothing.
	results = slices.DeleteFunc(results, func(r EraseTableResult) bool { return r.Mode == "" })
	return recs, results, nil
}

// eraseAnonymizeTable runs every anonymizer of one table as one UPDATE:
// each scrub column, and each match column, is set to its eraser's
// tombstone on the rows that eraser matches. The match column is scrubbed
// too, it IS the user-id reference being cut, and including it makes a
// re-run match zero rows (matching the DELETE plane's natural idempotency).
// One statement reads every match against the row as it was, so no
// anonymizer's tombstone hides a row from another. It returns, per eraser,
// the rows it matched.
func eraseAnonymizeTable(ctx context.Context, tx *sql.Tx, es []resolvedEraser) ([]int, error) {
	qt := quotePhysical(es[0].table)
	counts := make([]int, len(es))
	var args []any
	matches := make([]string, len(es))
	tombstones := make([]int, len(es)) // each eraser's tombstone placeholder
	type scrubbed struct {
		name string
		by   []int // indexes into es
	}
	var cols []*scrubbed
	byName := map[string]*scrubbed{}
	for i, e := range es {
		own, ownArgs := eraserMatch(e, 1)
		if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s", qt, own), ownArgs...).Scan(&counts[i]); err != nil {
			return nil, fmt.Errorf("framework: erase %q: %w", e.eraser.Name, err)
		}
		ts := e.eraser.Tombstone
		if ts == "" {
			ts = "[erased]"
		}
		// The eraser's match arguments, then its tombstone.
		match, margs := eraserMatch(e, len(args)+1)
		matches[i] = "(" + match + ")"
		args = append(append(args, margs...), ts)
		tombstones[i] = len(args)
		for _, c := range append(slices.Clone(e.eraser.ScrubColumns), e.eraser.Column) {
			s := byName[strings.ToLower(c)]
			if s == nil {
				s = &scrubbed{name: c}
				byName[strings.ToLower(c)] = s
				cols = append(cols, s)
			}
			if !slices.Contains(s.by, i) {
				s.by = append(s.by, i)
			}
		}
	}
	sets := make([]string, len(cols))
	for k, s := range cols {
		qc := query.QuoteFolded(query.MustIdent(s.name))
		whens := make([]string, len(s.by))
		for j, i := range s.by {
			whens[j] = fmt.Sprintf("WHEN %s THEN $%d", matches[i], tombstones[i])
		}
		sets[k] = fmt.Sprintf("%s = CASE %s ELSE %s END", qc, strings.Join(whens, " "), qc)
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s", qt, strings.Join(sets, ", "), strings.Join(matches, " OR "))
	if _, err := tx.ExecContext(ctx, stmt, args...); err != nil {
		names := make([]string, len(es))
		for i, e := range es {
			names[i] = e.eraser.Name
		}
		return nil, fmt.Errorf("framework: erase %s: %w", strings.Join(names, ", "), err)
	}
	return counts, nil
}

// eraseAnonymizeRaw is the shared UPDATE builder for the battery-plane and the
// built-in audit plane. qt is the table already quoted by the caller, which
// knows how the table was created. Each scrub column AND the match column
// are MustIdent-guarded; tombstone and userID are bound arguments.
func eraseAnonymizeRaw(ctx context.Context, tx *sql.Tx, qt, matchCol, userID, tombstone string, scrubCols ...string) (int, error) {
	qMatch := query.QuoteFolded(query.MustIdent(matchCol))
	sets := make([]string, len(scrubCols))
	for i, c := range scrubCols {
		// $1 is the tombstone, reused for every scrubbed column.
		sets[i] = query.QuoteFolded(query.MustIdent(c)) + " = $1"
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $2",
		qt, strings.Join(sets, ", "), qMatch)
	return execCount(ctx, tx, stmt, tombstone, userID)
}

// requireColumn fails when table has no column named col. SQLite reads a
// double-quoted unknown identifier as a STRING LITERAL, so
// `DELETE FROM "t" WHERE "owner_id" = $1` against a table with no owner_id
// deletes nothing and reports success, an erasure that silently erases
// nothing is the one outcome a right-to-be-forgotten primitive must never
// produce. Postgres rejects the unknown column itself; this makes both
// engines behave the same way.
func requireColumn(ctx context.Context, db *sql.DB, table, col string, dialect migrate.Dialect) error {
	return requireColumns(ctx, db, table, []string{col}, dialect)
}

// requireColumns is requireColumn for several columns, reading the catalog
// once.
func requireColumns(ctx context.Context, db *sql.DB, table string, cols []string, dialect migrate.Dialect) error {
	live, err := migrate.ReadLiveColumns(ctx, db, table, dialect)
	if err != nil {
		return fmt.Errorf("read columns of %q: %w", table, err)
	}
	if len(live) == 0 {
		return nil // table absent or unreadable, the caller's existence probe owns that case
	}
	for _, col := range cols {
		found := false
		for name := range live {
			if strings.EqualFold(name, col) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("table %q has no column %q: erasure would silently match nothing", table, col)
		}
	}
	return nil
}
