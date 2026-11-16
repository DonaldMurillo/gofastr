package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// EraseOption configures EraseUserData.
type EraseOption func(*eraseConfig)

type eraseConfig struct {
	dryRun         bool
	auditTable     string
	auditTombstone string
	tenant         string
	tenantSet      bool
}

// WithEraseTenant names the erased user's tenant for MemberSeparator
// erasers, which then remove only that tenant's and the bare entries.
// Without it the context's tenant is used; "" removes every tenant's entry.
func WithEraseTenant(tenantID string) EraseOption {
	return func(c *eraseConfig) { c.tenant, c.tenantSet = tenantID, true }
}

// WithEraseDryRun runs the erasure in a transaction it rolls back: the report
// and any refusal are the real run's, and no row or stored object is removed.
func WithEraseDryRun() EraseOption {
	return func(c *eraseConfig) { c.dryRun = true }
}

// WithEraseAuditTable names the audit table the audit plane anonymizes,
// "audit_log" by default; set it when AuditConfig.Table renamed it.
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
	Name         string // entity or eraser name; the table for a pivot
	Source       string // "entity" | "cascade" | battery source | "audit"
	Table        string // physical table
	Mode         string // "delete" | "anonymize" | "remove"
	RowsAffected int
}

// EraseReport is the structured summary returned by EraseUserData.
type EraseReport struct {
	DryRun   bool
	Entities []EraseTableResult // the rows the user owns, per owner-scoped table
	// Cascaded lists each table the cascade reaches, with the rows deleted
	// because a foreign key named an erased row; owned rows are not counted.
	Cascaded  []EraseTableResult
	Batteries []EraseTableResult // registered erasers (delete or anonymize)
	// Audit is the audit plane, nil when the audit table is absent.
	Audit *EraseTableResult
	// Stores lists the datexport.StoreErasers that succeeded.
	Stores  []EraseTableResult
	Skipped []string // erasers whose identity resolved to no value
}

const erasedAuditDiff = `{"erased":true}`

// TotalErased returns the sum of every RowsAffected across all planes.
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

// EraseUserData hard-deletes every row userID owns, every row a foreign key
// cascade reaches from them, the registered erasers' rows and the stored
// objects only they named, and anonymizes the user in the audit log, in one
// transaction. It errors on a blank id and on a relation that refuses it.
func (a *App) EraseUserData(ctx context.Context, userID string, opts ...EraseOption) (EraseReport, error) {
	if a == nil {
		return EraseReport{}, fmt.Errorf("framework: EraseUserData on nil App")
	}
	if a.DB == nil {
		return EraseReport{}, fmt.Errorf("framework: EraseUserData requires App.DB")
	}
	if strings.TrimSpace(userID) == "" {
		return EraseReport{}, fmt.Errorf("framework: EraseUserData requires a non-empty user id")
	}
	cfg := eraseConfig{auditTable: "audit_log", auditTombstone: "[erased]"}
	for _, o := range opts {
		o(&cfg)
	}

	dialect := migrate.DetectDialect(a.DB)
	erasers := datexport.AllErasers()

	user, err := a.resolveErasedUser(ctx, dialect, userID, erasers)
	if err != nil {
		return EraseReport{}, err
	}
	resolved, skipped := resolveEraserIdentities(user, erasers)
	tenant := cfg.tenant
	if !cfg.tenantSet {
		tenant = handler.GetTenantID(ctx)
	}
	for i := range resolved {
		resolved[i].tenant = tenant
	}
	plan, err := a.planErase(ctx, dialect, resolved)
	if err != nil {
		return EraseReport{}, err
	}

	rep, werr := a.eraseWrite(ctx, dialect, user, cfg, plan, resolved, cfg.dryRun)
	if werr == nil {
		rep.Skipped = skipped
	}
	return rep, werr
}

type resolvedEraser struct {
	eraser datexport.DataEraser
	value  string
	table  string
	tenant string
}

func (a *App) resolveErasedUser(ctx context.Context, dialect migrate.Dialect, userID string, erasers []datexport.DataEraser) (datexport.ErasedUser, error) {
	user := datexport.ErasedUser{ID: userID, Identities: map[datexport.IdentityKind]string{}}
	kinds := map[datexport.IdentityKind]string{}
	for _, e := range erasers {
		if e.Identity == datexport.IdentityUserID {
			continue
		}
		if _, ok := datexport.ResolveIdentity(e.Identity); !ok {
			return user, fmt.Errorf("framework: erase %q declares identity %d but no resolver is registered: erasure would be incomplete", e.Name, e.Identity)
		}
		if _, seen := kinds[e.Identity]; !seen {
			kinds[e.Identity] = fmt.Sprintf("erase %q", e.Name)
		}
	}
	if slices.ContainsFunc(datexport.AllStoreErasers(), func(s datexport.StoreEraser) bool { return s.EraseResolved != nil }) {
		for kind := range datexport.AllIdentityResolvers() {
			if _, seen := kinds[kind]; !seen {
				kinds[kind] = fmt.Sprintf("identity %d for the store erasers", kind)
			}
		}
	}
	for _, kind := range slices.Sorted(maps.Keys(kinds)) {
		r, _ := datexport.ResolveIdentity(kind)
		val, found, err := a.resolveIdentityValue(ctx, dialect, userID, r)
		if err != nil {
			return user, fmt.Errorf("framework: %s: resolve identity: %w", kinds[kind], err)
		}
		if found {
			user.Identities[kind] = val
		}
	}
	return user, nil
}

func resolveEraserIdentities(user datexport.ErasedUser, erasers []datexport.DataEraser) (resolved []resolvedEraser, skipped []string) {
	for _, e := range erasers {
		if e.Identity == datexport.IdentityUserID {
			resolved = append(resolved, resolvedEraser{eraser: e, value: e.ValuePrefix + user.ID})
			continue
		}
		val, ok := user.Identity(e.Identity)
		if !ok {
			skipped = append(skipped, e.Name)
			continue
		}
		resolved = append(resolved, resolvedEraser{eraser: e, value: e.ValuePrefix + val})
	}
	return resolved, skipped
}

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
	if r.Canonicalize != nil {
		if c := r.Canonicalize(val.String); c != "" {
			return c, true, nil
		}
	}
	return val.String, true, nil
}

type dataTable struct {
	name        string
	table       string
	key         []string
	entities    []string
	pivot       bool
	unmanaged   bool
	owner       string
	ownerName   string
	fileCols    []string
	variantCols []string
}

type fkEdge struct {
	child, parent string
	cols, key     []string
	setNull       bool
	blocks        entity.OnDeleteAction
	label         string
}

func blockingAction(action entity.OnDeleteAction) entity.OnDeleteAction {
	if action == entity.OnDeleteRestrict || action == entity.OnDeleteNoAction {
		return action
	}
	return ""
}

type fkEdgeID struct{ child, parent, cols string }

type dataGraph struct {
	tables map[string]*dataTable
	sorted []string
	edges  []fkEdge
}

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
		t := asciicase.Lower(ent.GetTable())
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
				continue
			}
			key := target.KeyColumns()
			if len(rel.ForeignKey) != len(key) {
				return nil, fmt.Errorf("framework: entity %q relation %q: foreign key (%s) does not match %s's key (%s), so its rows cannot be followed",
					n, rel.Name, strings.Join(rel.ForeignKey, ", "), rel.Entity, strings.Join(key, ", "))
			}
			e := fkEdge{
				child: asciicase.Lower(ent.GetTable()), parent: asciicase.Lower(target.GetTable()),
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
		pt, src, dst := asciicase.Lower(p.Table), asciicase.Lower(p.SourceTable), asciicase.Lower(p.TargetTable)
		if g.tables[pt] != nil {
			continue
		}
		g.tables[pt] = &dataTable{name: p.Table, table: pt, key: []string{p.LocalKey, p.TargetFK}, pivot: true}
		g.edges = append(g.edges,
			fkEdge{child: pt, parent: src, cols: []string{p.LocalKey}, key: []string{p.SourceKey}, label: p.Table},
			fkEdge{child: pt, parent: dst, cols: []string{p.TargetFK}, key: []string{p.TargetKey}, label: p.Table},
		)
	}
	g.sorted = slices.Sorted(maps.Keys(g.tables))
	return g, nil
}

func orderTables(tables []string, edges []fkEdge, parentsFirst bool) []string {
	in := make(map[string]bool, len(tables))
	for _, t := range tables {
		in[t] = true
	}
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
	state := map[string]int{}
	var visit func(string)
	visit = func(t string) {
		if state[t] != 0 {
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

type erasePlanTable struct {
	dt                      *dataTable
	owner                   string
	tmp                     string
	reached                 bool
	seeds                   []resolvedEraser
	owned, seeded, cascaded int
}

func (pt *erasePlanTable) staged() int { return pt.owned + pt.seeded + pt.cascaded }

type erasePlan struct {
	tables      []*erasePlanTable
	edges       []fkEdge
	blockers    []fkEdge
	cuts        []fkEdge
	graph       map[string]*dataTable
	eraserGraph map[string]string
	seededBy    map[string]int
}

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
			continue
		}
		eraserGraph[e.eraser.Name] = t
		if e.eraser.Mode != datexport.EraseDelete {
			continue
		}
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
				continue
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
					absent[e.child] = true
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
	taken := map[string]bool{}
	for t := range g.tables {
		taken[strings.ToLower(t)] = true
	}
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
	pos := map[string]int{}
	for i, pt := range plan.tables {
		pos[pt.dt.table] = i
	}
	for _, e := range order {
		if len(byTable[e.child].dt.key) == 0 {
			continue
		}
		if e.child == e.parent && e.blocks == entity.OnDeleteRestrict || e.child != e.parent && pos[e.child] > pos[e.parent] {
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

func colsEqual(l string, lcols []string, r string, rcols []string) string {
	out := make([]string, len(lcols))
	for i := range lcols {
		out[i] = l + "." + query.QuoteFolded(query.MustIdent(lcols[i])) + " = " +
			r + "." + query.QuoteFolded(query.MustIdent(rcols[i]))
	}
	return strings.Join(out, " AND ")
}

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

func (p *erasePlan) stage(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, userID string) error {
	for _, pt := range p.tables {
		if pt.tmp == "" {
			continue
		}
		qtmp := query.QuoteFolded(query.MustIdent(pt.tmp))
		qt := query.QuoteFolded(query.MustIdent(pt.dt.table))
		var stmts []string
		if dialect == migrate.DialectPostgres {
			stmts = []string{fmt.Sprintf("CREATE TEMP TABLE %s ON COMMIT DROP AS SELECT %s FROM %s WITH NO DATA",
				qtmp, quotedCols("", pt.dt.key), qt)}
		} else {
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

func anyNull(alias string, cols []string) string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = alias + "." + query.QuoteFolded(query.MustIdent(c)) + " IS NULL"
	}
	return "(" + strings.Join(out, " OR ") + ")"
}

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

type auditRecordSet struct {
	entities []string
	ids      []string
}

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
		id = sanitizeAuditField(id)
		ids = append(ids, id, auditKeyDigest(id))
	}
	return ids, rows.Err()
}

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

func lockRead(dialect migrate.Dialect) string {
	if dialect == migrate.DialectPostgres {
		return " FOR UPDATE"
	}
	return ""
}

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

type auditShape struct {
	table   string
	diff    bool
	records bool
}

func (at auditShape) quoted() string { return query.QuoteFolded(query.MustIdent(at.table)) }

const auditRecordChunk = 500

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

func eraseAuditPlane(ctx context.Context, tx *sql.Tx, at auditShape, userID, tombstone string, recs []auditRecordSet) (int, error) {
	qt := at.quoted()
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
	err = recordChunks(recs, 2, func(clause string, args []any) error {
		c, err := execCount(ctx, tx, fmt.Sprintf("UPDATE %s SET diff = $1 WHERE diff IS NOT NULL AND diff <> $1 AND %s", qt, clause),
			append([]any{erasedAuditDiff}, args...)...)
		n += c
		return err
	})
	return n, err
}

func (a *App) eraseWrite(ctx context.Context, dialect migrate.Dialect, user datexport.ErasedUser, cfg eraseConfig, plan *erasePlan, erasers []resolvedEraser, dryRun bool) (EraseReport, error) {
	userID := user.ID
	report := EraseReport{DryRun: dryRun}

	liveErasers := make([]resolvedEraser, 0, len(erasers))
	for _, e := range erasers {
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

	if err := plan.stage(ctx, tx, dialect, userID); err != nil {
		return report, err
	}
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
		committed = true
		if err := tx.Rollback(); err != nil {
			return report, fmt.Errorf("framework: erase dry run: rollback: %w", err)
		}
		return report, a.eraseStores(ctx, user, true, &report)
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("framework: erase: commit: %w", err)
	}
	committed = true

	var objErr error
	if deletable, err := a.unsharedObjectKeys(ctx, dialect, storedKeys); err != nil {
		objErr = fmt.Errorf("framework: erase: rows deleted but no stored objects were removed: checking which keys other rows still name: %w", err)
	} else {
		objErr = a.eraseStoredObjects(ctx, deletable)
	}
	return report, errors.Join(objErr, a.eraseStores(ctx, user, false, &report))
}

func (a *App) eraseStores(ctx context.Context, user datexport.ErasedUser, dryRun bool, rep *EraseReport) error {
	var failed []string
	for _, s := range datexport.AllStoreErasers() {
		n, err := runStoreEraser(ctx, s, user, dryRun)
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

func runStoreEraser(ctx context.Context, s datexport.StoreEraser, user datexport.ErasedUser, dryRun bool) (n int, err error) {
	defer func() {
		if v := recover(); v != nil {
			n, err = 0, fmt.Errorf("panicked: %s", textsafe.Recovered(v))
		}
	}()
	if s.EraseResolved != nil {
		return s.EraseResolved(ctx, user, dryRun)
	}
	return s.Erase(ctx, user.ID, dryRun)
}

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

func eraseDelete(ctx context.Context, tx *sql.Tx, e resolvedEraser) (int, error) {
	match, args := eraserMatch(e, 1)
	return execCount(ctx, tx, fmt.Sprintf("DELETE FROM %s WHERE %s", quotePhysical(e.table), match), args...)
}

func eraseRemoveMember(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, e resolvedEraser) (int, error) {
	qt := quotePhysical(e.table)
	qc := query.QuoteFolded(query.MustIdent(e.eraser.Column))
	args := []any{e.value}
	matches := func(v string) string { return fmt.Sprintf("%s = $1", v) }
	if sep := e.eraser.MemberSeparator; sep != "" && e.tenant != "" {
		args = append(args, sep+e.value, memberQualifier(e.tenant, sep)+sep+e.value)
		matches = func(v string) string { return fmt.Sprintf("%s IN ($1, $2, $3)", v) }
	} else if sep != "" {
		args = append(args, sep)
		find, width := "instr(%s, $2)", "length($2)"
		if dialect == migrate.DialectPostgres {
			find, width = "strpos(%s, $2::text)", "length($2::text)"
		}
		matches = func(v string) string {
			at := fmt.Sprintf(find, v)
			return fmt.Sprintf("(%[1]s = $1 OR (CASE WHEN %[2]s > 0 THEN substr(%[1]s, %[2]s + %[3]s) ELSE %[1]s END) = $1)", v, at, width)
		}
	}
	var rest, holds string
	if dialect == migrate.DialectPostgres {
		rest = fmt.Sprintf("COALESCE((SELECT json_agg(m.v ORDER BY m.n)::text FROM json_array_elements_text(%s::json) WITH ORDINALITY AS m(v, n) WHERE NOT %s), '[]')", qc, matches("m.v"))
		holds = fmt.Sprintf("EXISTS (SELECT 1 FROM json_array_elements_text(%s::json) AS h(v) WHERE %s)", qc, matches("h.v"))
	} else {
		rest = fmt.Sprintf("(SELECT json_group_array(m.value) FROM (SELECT value FROM json_each(%s) WHERE NOT %s ORDER BY key) AS m)", qc, matches("value"))
		holds = fmt.Sprintf("EXISTS (SELECT 1 FROM json_each(%s) WHERE %s)", qc, matches("value"))
	}
	conds, wargs := whereConds(e.eraser, len(args)+1)
	if conds != "" {
		holds += " AND " + conds
	}
	return execCount(ctx, tx, fmt.Sprintf("UPDATE %s SET %s = %s WHERE %s", qt, qc, rest, holds), append(args, wargs...)...)
}

func memberQualifier(tenantID, sep string) string {
	q := strings.ReplaceAll(tenantID, "%", "%25")
	var esc strings.Builder
	for i := range len(sep) {
		fmt.Fprintf(&esc, "%%%02X", sep[i])
	}
	return strings.ReplaceAll(q, sep, esc.String())
}

func eraserMatch(e resolvedEraser, first int) (string, []any) {
	match := fmt.Sprintf("%s = $%d", query.QuoteFolded(query.MustIdent(e.eraser.Column)), first)
	conds, args := whereConds(e.eraser, first+1)
	if conds != "" {
		match += " AND " + conds
	}
	return match, append([]any{e.value}, args...)
}

func whereConds(e datexport.DataEraser, first int) (string, []any) {
	conds := make([]string, len(e.Where))
	args := make([]any, len(e.Where))
	for i, w := range e.Where {
		conds[i] = fmt.Sprintf("%s = $%d", query.QuoteFolded(query.MustIdent(w.Column)), first+i)
		args[i] = w.Value
	}
	return strings.Join(conds, " AND "), args
}

func whereColumns(e datexport.DataEraser) []string {
	cols := make([]string, len(e.Where))
	for i, w := range e.Where {
		cols[i] = w.Column
	}
	return cols
}

var eraseKeysRead func()

func (p *erasePlan) eraseBatteries(ctx context.Context, tx *sql.Tx, dialect migrate.Dialect, erasers []resolvedEraser, wantRecs bool, keys *objectKeys) ([]auditRecordSet, []EraseTableResult, error) {
	var recs []auditRecordSet
	var anonTables []string
	anon := map[string][]int{}
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
	results = slices.DeleteFunc(results, func(r EraseTableResult) bool { return r.Mode == "" })
	return recs, results, nil
}

func eraseAnonymizeTable(ctx context.Context, tx *sql.Tx, es []resolvedEraser) ([]int, error) {
	qt := quotePhysical(es[0].table)
	counts := make([]int, len(es))
	var args []any
	matches := make([]string, len(es))
	tombstones := make([]int, len(es))
	type scrubbed struct {
		name string
		by   []int
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

func eraseAnonymizeRaw(ctx context.Context, tx *sql.Tx, qt, matchCol, userID, tombstone string, scrubCols ...string) (int, error) {
	qMatch := query.QuoteFolded(query.MustIdent(matchCol))
	sets := make([]string, len(scrubCols))
	for i, c := range scrubCols {
		sets[i] = query.QuoteFolded(query.MustIdent(c)) + " = $1"
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $2",
		qt, strings.Join(sets, ", "), qMatch)
	return execCount(ctx, tx, stmt, tombstone, userID)
}

func requireColumn(ctx context.Context, db *sql.DB, table, col string, dialect migrate.Dialect) error {
	return requireColumns(ctx, db, table, []string{col}, dialect)
}

func requireColumns(ctx context.Context, db *sql.DB, table string, cols []string, dialect migrate.Dialect) error {
	live, err := migrate.ReadLiveColumns(ctx, db, table, dialect)
	if err != nil {
		return fmt.Errorf("read columns of %q: %w", table, err)
	}
	if len(live) == 0 {
		return nil
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
