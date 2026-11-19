package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	fwdb "github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// View is a declared database view: a SELECT over entity tables, created
// after its source tables and optionally queried read-only through the ORM
// (App.View).
type View struct {
	Name         string   // view name; also the registry key / table name for queries
	Select       string   // the SELECT body: "SELECT u.id, u.name FROM users u WHERE u.active"
	DependsOn    []string // source table/view names, for create-after ordering
	Columns      []Column // output columns, for the read-only ORM entity + OpenAPI
	Materialized bool     // Postgres MATERIALIZED VIEW (a plain VIEW otherwise; ignored on SQLite)
}

// render returns the dialect's forward (up) and inverse (down) DDL; it
// panics on a View.Name that is not a safe identifier.
func (v View) render(dialect Dialect) (up, down string) {
	name, err := query.SafeIdent(v.Name)
	if err != nil {
		panic(fmt.Sprintf("migrate: view name %q is not a valid SQL identifier: %v", v.Name, err))
	}
	name = ddlIdent(name)
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

func (v View) routine(dialect Dialect) Routine {
	up, down := v.render(dialect)
	return Routine{Name: v.Name, Up: up, Down: down}
}

// ToEntity builds the read-only, Unmanaged ORM entity for a view from its
// Columns, keyed on the columns marked PrimaryKey in column order. It returns
// nil when no columns are declared and panics when none is a key.
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
		Scope:      &entity.ScopeConfig{},
		Pagination: &entity.PaginationConfig{},
		Exposure:   &entity.ExposureConfig{},
	}}
	return ent
}

// applyViews runs every view's Up at boot, a view after the views it
// depends on.
func applyViews(ctx context.Context, tx *sql.Tx, views []View, dialect Dialect) error {
	ordered := topoSortViews(views)
	dependents := viewDependents(ordered)
	for _, v := range ordered {
		if dialect == DialectPostgres {
			stale, err := pgStaleViewKind(ctx, tx, v)
			if err != nil {
				return fmt.Errorf("migrate view %s: %w", v.Name, err)
			}
			if v.Materialized || stale != "" {
				for _, d := range slices.Backward(dependents[v.Name]) {
					if err := pgDropView(ctx, tx, d); err != nil {
						return fmt.Errorf("migrate view %s: drop dependent view %s: %w", v.Name, d.Name, err)
					}
				}
			}
			if stale != "" {
				if _, err := tx.ExecContext(ctx, stale); err != nil {
					return fmt.Errorf("migrate view %s: drop the old kind: %w", v.Name, err)
				}
			}
		}
		if dialect == DialectPostgres && !v.Materialized {
			if err := pgReplaceView(ctx, tx, v, dependents[v.Name]); err != nil {
				return fmt.Errorf("migrate view %s: %w", v.Name, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, v.routine(dialect).Up); err != nil {
			return fmt.Errorf("migrate view %s: %w", v.Name, err)
		}
	}
	return nil
}

// pgReplaceView runs a plain Postgres view's CREATE OR REPLACE VIEW and, when
// Postgres refuses it, drops the view and creates it again.
func pgReplaceView(ctx context.Context, tx *sql.Tx, v View, dependents []View) error {
	up, down := v.render(DialectPostgres)
	var replaceErr error
	err := fwdb.InSavepoint(ctx, tx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, up); err != nil {
			// Give the statement error its own identity for the retry check.
			replaceErr = fmt.Errorf("%w", err)
		}
		return replaceErr
	})
	if err == nil || err != replaceErr || ctx.Err() != nil {
		return err
	}
	return fwdb.InSavepoint(ctx, tx, func(ctx context.Context, tx *sql.Tx) error {
		recreate := func() error {
			for _, d := range slices.Backward(dependents) {
				if err := pgDropView(ctx, tx, d); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, down); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, strings.Replace(up, "CREATE OR REPLACE VIEW", "CREATE VIEW", 1))
			return err
		}
		if err := recreate(); err != nil {
			return fmt.Errorf("%w (dropping the view to recreate it also failed: %v)", replaceErr, err)
		}
		return nil
	})
}

// pgLiveViewKind returns "v", "m", or "" for the live relation named like v.
func pgLiveViewKind(ctx context.Context, q execQueryer, v View) (string, error) {
	rows, err := q.QueryContext(ctx, `SELECT relkind::text FROM pg_class WHERE oid = to_regclass($1) AND relkind IN ('v', 'm')`, ddlIdent(v.Name))
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

// topoSortViews orders views so a view comes after the views it depends on;
// table dependencies are ignored and cycles fall back to name order.
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
			return
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
