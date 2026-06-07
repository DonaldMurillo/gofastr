package migrate

import (
	"fmt"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// View is a virtual table built from other entities — a read model defined by
// a SELECT over entity tables. It belongs to both the migration story (created
// after its source tables, reversible, checksum-tracked in generate) and the
// ORM story (register it with App.View to query it read-only). A View is the
// "virtual table that takes in other entities to be constructed".
type View struct {
	Name         string   // view name; also the registry key / table name for queries
	Select       string   // the SELECT body: "SELECT u.id, u.name FROM users u WHERE u.active"
	DependsOn    []string // source table/view names — for create-after ordering
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
	// unsafe name is a misconfiguration — fail loud, consistent with ToEntity.
	// View.Select is intentionally free-form developer-authored SQL (the read
	// model's query body), so it is not — and cannot be — identifier-escaped.
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
// for it — the view DDL handles its existence), so registering it only adds the
// ability to query the view through the ORM. Returns nil when no columns are
// declared (the view is then migration-only — query it with raw SQL).
func (v View) ToEntity() *entity.Entity {
	if len(v.Columns) == 0 {
		return nil
	}
	fields := make([]schema.Field, 0, len(v.Columns))
	pk := ""
	pkCount := 0
	for _, c := range v.Columns {
		fields = append(fields, schema.Field{
			Name:    c.Name,
			Type:    c.Type,
			RawType: c.RawType,
		})
		if c.PrimaryKey {
			pk = c.Name
			pkCount++
		}
	}
	// A view exposed through the ORM needs exactly one primary-key column so
	// GET /{view}/{id} resolves; fail loud rather than mounting a broken route.
	if pk == "" {
		panic(fmt.Sprintf("migrate: view %q declares Columns but no PrimaryKey column — mark one column PrimaryKey: true (it's the id the ORM reads), or drop Columns to keep the view migration-only", v.Name))
	}
	if pkCount > 1 {
		panic(fmt.Sprintf("migrate: view %q marks %d columns PrimaryKey — exactly one is required", v.Name, pkCount))
	}
	ent := &entity.Entity{Config: entity.EntityConfig{
		Name:      v.Name,
		Table:     v.Name,
		Fields:    fields,
		Unmanaged: true,
	}}
	ent.PrimaryKey = pk
	return ent
}

// topoSortViews orders views so that a view depending on another view (via
// DependsOn) is created after it. Dependencies on plain tables are ignored
// here — all views are emitted after all tables regardless. Cycles are broken
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
			return // a table dependency, not a view — ignore
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
