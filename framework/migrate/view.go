package migrate

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// View is a virtual table built from other entities, a read model defined by
// a SELECT over entity tables. It belongs to both the migration story (created
// after its source tables, reversible, checksum-tracked in generate) and the
// ORM story (register it with App.View to query it read-only). A View is the
// "virtual table that takes in other entities to be constructed".
type View struct {
	Name         string   // view name; also the registry key / table name for queries
	Select       string   // the SELECT body: "SELECT u.id, u.name FROM users u WHERE u.active"
	DependsOn    []string // source table/view names, for create-after ordering
	Columns      []Column // output columns, for the read-only ORM entity + OpenAPI
	Materialized bool     // Postgres MATERIALIZED VIEW (a plain VIEW otherwise; ignored on SQLite)
}

// render returns the dialect-specific forward (up) and inverse (down) DDL.
//
//   - Postgres plain view: CREATE OR REPLACE VIEW (idempotent, update-in-place).
//   - Postgres materialized / SQLite: DROP … IF EXISTS then CREATE (idempotent
//     and update-capable; SQLite has neither OR REPLACE nor materialized views).
func (v View) render(dialect Dialect) (up, down string) {
	// View.Name is interpolated into DDL as an identifier, so it must be a
	// safe identifier. It comes from developer code (not request input), so an
	// unsafe name is a misconfiguration. Fail loud, consistent with ToEntity.
	// View.Select is intentionally free-form developer-authored SQL (the read
	// model's query body), so it is not, and cannot be, identifier-escaped.
	name, err := query.SafeIdent(v.Name)
	if err != nil {
		panic(fmt.Sprintf("migrate: view name %q is not a valid SQL identifier: %v", v.Name, err))
	}
	kind := "VIEW"
	materialized := v.Materialized && dialect == DialectPostgres
	if materialized {
		kind = "MATERIALIZED VIEW"
	}
	down = fmt.Sprintf("DROP %s IF EXISTS %s", kind, name)
	if dialect == DialectPostgres && !materialized {
		up = fmt.Sprintf("CREATE OR REPLACE VIEW %s AS %s", name, v.Select)
		return up, down
	}
	up = fmt.Sprintf("%s;\nCREATE %s %s AS %s", down, kind, name, v.Select)
	return up, down
}

// routine adapts a View into the Routine shape so it flows through the same
// idempotent-apply and reversible-generate machinery as stored routines.
func (v View) routine(dialect Dialect) Routine {
	up, down := v.render(dialect)
	return Routine{Name: v.Name, Up: up, Down: down}
}

// ToEntity builds the read-only ORM entity for a view from its declared
// Columns. The entity is Unmanaged (the migration system never emits table DDL
// for it. The view DDL handles its existence), so registering it only adds the
// ability to query the view through the ORM. Returns nil when no columns are
// declared (the view is then migration-only: query it with raw SQL).
func (v View) ToEntity() *entity.Entity {
	if len(v.Columns) == 0 {
		return nil
	}
	fields := make([]schema.Field, 0, len(v.Columns))
	var pk []string
	for _, c := range v.Columns {
		fields = append(fields, schema.Field{
			Name:    c.Name,
			Type:    c.Type,
			RawType: c.RawType,
		})
		if c.PrimaryKey {
			pk = append(pk, c.Name)
		}
	}
	// A view exposed through the ORM needs a key so GET /{view}/{id}
	// resolves; fail loud rather than mounting a broken route. Several
	// marked columns make a composite key, in column order.
	if len(pk) == 0 {
		panic(fmt.Sprintf("migrate: view %q declares Columns but no PrimaryKey column: mark its key columns PrimaryKey: true (the id the ORM reads), or drop Columns to keep the view migration-only", v.Name))
	}
	timestamps := false
	ent := &entity.Entity{Config: entity.EntityConfig{
		Name:       v.Name,
		Table:      v.Name,
		Fields:     fields,
		PrimaryKey: pk,
		Unmanaged:  true,
		Timestamps: &timestamps,
		// Materialized because this constructor bypasses Define()'s
		// normalization. The diff engine reads Config.Scope directly.
		Scope:      &entity.ScopeConfig{},
		Pagination: &entity.PaginationConfig{},
		Exposure:   &entity.ExposureConfig{},
	}}
	return ent
}

// applyViews runs every view's Up at boot, a view after the views it
// depends on. A plain view replaces itself in place on Postgres, and SQLite
// drops a view other views read without complaint.
//
// A Postgres materialized view cannot be replaced in place, so its Up drops
// and recreates it (which also refreshes its rows), and Postgres refuses to
// drop one another view reads: the second boot of any plan with a view over
// a materialized one failed. Before recreating a materialized view, boot
// drops the plan's views that depend on it, deepest first; each comes after
// it in dependency order and is recreated in the same pass. A dependent view
// the plan does not declare still blocks the drop, and the boot fails
// naming the materialized view.
func applyViews(ctx context.Context, exec execQueryer, views []View, dialect Dialect) error {
	ordered := topoSortViews(views)
	dependents := viewDependents(ordered)
	for _, v := range ordered {
		if dialect == DialectPostgres {
			stale, err := pgStaleViewKind(ctx, exec, v)
			if err != nil {
				return fmt.Errorf("migrate view %s: %w", v.Name, err)
			}
			if v.Materialized || stale != "" {
				for _, d := range slices.Backward(dependents[v.Name]) {
					if err := pgDropView(ctx, exec, d); err != nil {
						return fmt.Errorf("migrate view %s: drop dependent view %s: %w", v.Name, d.Name, err)
					}
				}
			}
			if stale != "" {
				// The declared kind's Up refuses the other kind's object.
				if _, err := exec.ExecContext(ctx, stale); err != nil {
					return fmt.Errorf("migrate view %s: drop the old kind: %w", v.Name, err)
				}
			}
		}
		if dialect == DialectPostgres && !v.Materialized {
			if err := pgReplaceView(ctx, exec, v, dependents[v.Name]); err != nil {
				return fmt.Errorf("migrate view %s: %w", v.Name, err)
			}
			continue
		}
		if _, err := exec.ExecContext(ctx, v.routine(dialect).Up); err != nil {
			return fmt.Errorf("migrate view %s: %w", v.Name, err)
		}
	}
	return nil
}

// pgReplaceView runs a plain Postgres view's CREATE OR REPLACE VIEW, and
// when Postgres refuses it, drops the view and creates it again. CREATE OR
// REPLACE keeps the view's grants but refuses a body whose columns are not
// the old ones plus new ones at the end ("cannot drop columns from view",
// "cannot change name of view column"), so a view that lost, renamed or
// retyped a column failed every boot from then on, while SQLite, which
// always drops and creates, booted. The statement runs under a savepoint,
// since a failed one aborts the boot's transaction. The plan's views that
// read v are dropped first, deepest first, and the loop recreates them
// after v. A view outside the plan that reads v still refuses the DROP,
// and then the boot fails with CREATE OR REPLACE's own error.
func pgReplaceView(ctx context.Context, exec execQueryer, v View, dependents []View) error {
	up, down := v.render(DialectPostgres)
	if _, err := exec.ExecContext(ctx, "SAVEPOINT gofastr_view"); err != nil {
		return err
	}
	_, replaceErr := exec.ExecContext(ctx, up)
	if replaceErr == nil {
		_, err := exec.ExecContext(ctx, "RELEASE SAVEPOINT gofastr_view")
		return err
	}
	if _, err := exec.ExecContext(ctx, "ROLLBACK TO SAVEPOINT gofastr_view"); err != nil {
		return replaceErr
	}
	recreate := func() error {
		for _, d := range slices.Backward(dependents) {
			if err := pgDropView(ctx, exec, d); err != nil {
				return err
			}
		}
		if _, err := exec.ExecContext(ctx, down); err != nil {
			return err
		}
		_, err := exec.ExecContext(ctx, strings.Replace(up, "CREATE OR REPLACE VIEW", "CREATE VIEW", 1))
		return err
	}
	if err := recreate(); err != nil {
		_, _ = exec.ExecContext(ctx, "ROLLBACK TO SAVEPOINT gofastr_view")
		return fmt.Errorf("%w (dropping the view to recreate it also failed: %v)", replaceErr, err)
	}
	_, err := exec.ExecContext(ctx, "RELEASE SAVEPOINT gofastr_view")
	return err
}

// pgLiveViewKind returns the kind of the live relation named like v: "v"
// for a plain view, "m" for a materialized one, "" when there is none
// (or it is some other kind of relation, which the view's Up reports).
func pgLiveViewKind(ctx context.Context, q execQueryer, v View) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT relkind::text FROM pg_class WHERE oid = to_regclass($1) AND relkind IN ('v', 'm')`, v.Name)
	if err != nil {
		return "", fmt.Errorf("read the live kind: %w", err)
	}
	defer rows.Close()
	kind := ""
	if rows.Next() {
		if err := rows.Scan(&kind); err != nil {
			return "", fmt.Errorf("read the live kind: %w", err)
		}
	}
	return kind, rows.Err()
}

// pgStaleViewKind returns the statement that drops the live relation named
// like v when it is the other kind of view, or "" when it is absent or
// already v's kind.
func pgStaleViewKind(ctx context.Context, q execQueryer, v View) (string, error) {
	live, err := pgLiveViewKind(ctx, q, v)
	if err != nil || live == "" {
		return "", err
	}
	other := v
	other.Materialized = live == "m"
	if other.Materialized == v.Materialized {
		return "", nil
	}
	_, down := other.render(DialectPostgres)
	return down, nil
}

// pgDropView drops the live relation named like v with the statement for
// the kind it is, which may not be the kind v declares.
func pgDropView(ctx context.Context, exec execQueryer, v View) error {
	live, err := pgLiveViewKind(ctx, exec, v)
	if err != nil || live == "" {
		return err
	}
	as := v
	as.Materialized = live == "m"
	_, down := as.render(DialectPostgres)
	_, err = exec.ExecContext(ctx, down)
	return err
}

// viewDependents maps each view to the views that read it, directly or
// through another view, in the dependency order of ordered.
func viewDependents(ordered []View) map[string][]View {
	pos := make(map[string]int, len(ordered))
	for i, v := range ordered {
		pos[v.Name] = i
	}
	out := map[string][]View{}
	for i, v := range ordered {
		// reads holds every view v reads, transitively.
		reads := map[string]bool{}
		var walk func(View)
		walk = func(x View) {
			for _, d := range x.DependsOn {
				j, ok := pos[d]
				if !ok || reads[d] {
					continue
				}
				reads[d] = true
				walk(ordered[j])
			}
		}
		walk(v)
		for _, d := range slices.Sorted(maps.Keys(reads)) {
			if d != v.Name {
				out[d] = append(out[d], ordered[i])
			}
		}
	}
	return out
}

// topoSortViews orders views so that a view depending on another view (via
// DependsOn) is created after it. Dependencies on plain tables are ignored
// here. All views are emitted after all tables regardless. Cycles are broken
// conservatively (name order); a view cannot truly depend on itself.
func topoSortViews(views []View) []View {
	byName := make(map[string]View, len(views))
	names := make([]string, 0, len(views))
	for _, v := range views {
		byName[v.Name] = v
		names = append(names, v.Name)
	}
	sort.Strings(names)

	visited := map[string]bool{}
	temp := map[string]bool{}
	out := make([]View, 0, len(views))
	var visit func(name string)
	visit = func(name string) {
		if visited[name] || temp[name] {
			return
		}
		v, ok := byName[name]
		if !ok {
			return // a table dependency, not a view: ignore
		}
		temp[name] = true
		deps := append([]string(nil), v.DependsOn...)
		sort.Strings(deps)
		for _, d := range deps {
			visit(d)
		}
		temp[name] = false
		visited[name] = true
		out = append(out, v)
	}
	for _, n := range names {
		visit(n)
	}
	return out
}
