package appspec

import (
	"database/sql"
	"strings"
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The generated app runs entityui.CheckStat and CheckChart on every
// sourced stat_card and chart at boot, over the entity it registered:
// the spec's fields plus the columns entity.Define adds (id, the
// timestamps, the tenant and soft-delete columns), none of which sets a
// WireName. The load judged a stat_card filter over the spec's fields
// alone, each given its camel spelling, and passed any filter that did
// not parse: dueOn for due_on, and a column the entity lacks, both
// loaded and then panicked at boot. The load now judges over the
// registered entity's fields, so its verdict is the boot verdict.
const bootParitySpec = `
app:
  name: Demo
  module: example.com/demo
entities:
  - name: cards
    crud: true
    soft_delete: true
    owner_field: owner_id
    fields:
      - name: label
        type: string
      - name: due_on
        type: date
      - name: stage
        type: enum
        values: [draft, open]
  - name: notes
    crud: true
    timestamps: false
    fields:
      - name: body
        type: string
screens:
  - name: dash
    route: /
    body:
`

func bootParityAppSpec(t *testing.T, block string) (AppSpec, error) {
	t.Helper()
	node, err := coreyaml.Parse(bootParitySpec + block)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	bp, err := DecodeAppSpec(node)
	if err != nil {
		return bp, err
	}
	return bp, ValidateAppSpec(bp)
}

func statFilterBlock(entity, filter string) string {
	return `      - kind: stat_card
        props:
          label: Cards
          source:
            entity: ` + entity + `
            filter: '` + filter + `'
`
}

func chartBlock(entity, groupBy string) string {
	return `      - kind: bar_chart
        props:
          title: Cards
          source:
            entity: ` + entity + `
            group_by: ` + groupBy + `
`
}

// bootUI registers the spec's entities the way the generated app does
// (app.Entity over each declaration's config) and returns the EntityUI
// the generated screens check their cards with at boot.
func bootUI(t *testing.T, bp AppSpec) *entityui.UI {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	for _, decl := range bp.Entities {
		cfg, err := decl.Config()
		if err != nil {
			t.Fatalf("entity %s: %v", decl.Name, err)
		}
		app.Entity(decl.Name, cfg)
	}
	return app.EntityUI(entityui.Extensions{})
}

func TestStatFilterLoadMatchesBoot(t *testing.T) {
	accepted := []struct{ entity, filter string }{
		{"cards", "stage=open"},
		{"cards", `due_on = "2026-01-01"`},
		{"cards", `created_at >= "2026-01-01T00:00:00Z"`},
		{"cards", `updated_at < "2026-01-01T00:00:00Z" and label contains "x"`},
		{"cards", `id = "8f14e45f-ceea-467f-a0e6-1a0c5b7d2b3e"`},
	}
	// A filter the boot parse refuses is refused at load naming the card
	// and the parse error; a hidden or no_query column keeps its own
	// reason.
	refused := []struct{ entity, filter, want string }{
		{"cards", "dueOn=2026-01-01", `stat_card "Cards"`},
		{"cards", "dueOn=2026-01-01", "dueOn"},
		{"cards", `nosuch = "x"`, `stat_card "Cards"`},
		{"cards", `nosuch = "x"`, "nosuch"},
		{"cards", `label = "x" and nosuch = "y"`, "nosuch"},
		{"cards", `stage ~ "x"`, `stat_card "Cards"`},
		{"cards", `owner_id = "u1"`, "owner_id"},
		{"cards", `deleted_at > "2026-01-01T00:00:00Z"`, "deleted_at"},
		{"notes", `created_at >= "2026-01-01T00:00:00Z"`, "created_at"},
	}
	for _, c := range accepted {
		bp, err := bootParityAppSpec(t, statFilterBlock(c.entity, c.filter))
		if err != nil {
			t.Errorf("%s filter %q was refused at load: %v", c.entity, c.filter, err)
			continue
		}
		// The decoded filter: decode rewrites the bare field=value spelling
		// into the DSL, and that is the text the generated app checks.
		where, _ := bp.Screens[0].Body[0].Props["source"].(map[string]any)["filter"].(string)
		if err := bootUI(t, bp).CheckStat(c.entity, "", "", where, ""); err != nil {
			t.Errorf("%s filter %q loads but fails the boot check: %v", c.entity, c.filter, err)
		}
	}
	for _, c := range refused {
		bp, err := bootParityAppSpec(t, statFilterBlock(c.entity, c.filter))
		if err == nil {
			t.Errorf("%s filter %q was accepted at load", c.entity, c.filter)
			continue
		}
		if !strings.Contains(err.Error(), "stat_card") || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s filter %q: the refusal does not name %s: %v", c.entity, c.filter, c.want, err)
		}
		where, _ := bp.Screens[0].Body[0].Props["source"].(map[string]any)["filter"].(string)
		if bootUI(t, bp).CheckStat(c.entity, "", "", where, "") == nil {
			t.Errorf("%s filter %q passes the boot check, so the load refuses a card that works", c.entity, c.filter)
		}
	}
}

func TestChartGroupByLoadMatchesBoot(t *testing.T) {
	for _, c := range [][2]string{{"cards", "stage"}, {"cards", "created_at"}, {"cards", "due_on"}} {
		bp, err := bootParityAppSpec(t, chartBlock(c[0], c[1]))
		if err != nil {
			t.Errorf("%s group_by %q was refused at load: %v", c[0], c[1], err)
			continue
		}
		if err := bootUI(t, bp).CheckChart(c[0], c[1]); err != nil {
			t.Errorf("%s group_by %q loads but fails the boot check: %v", c[0], c[1], err)
		}
	}
	// The owner column the load adds and the soft-delete column Define adds are hidden, so the
	// boot check refuses a chart grouped on them; a table without
	// timestamps has no created_at.
	for _, c := range [][2]string{{"cards", "owner_id"}, {"cards", "deleted_at"}, {"cards", "dueOn"}, {"notes", "created_at"}} {
		bp, err := bootParityAppSpec(t, chartBlock(c[0], c[1]))
		if err == nil {
			t.Errorf("%s group_by %q was accepted at load", c[0], c[1])
			continue
		}
		if !strings.Contains(err.Error(), c[1]) {
			t.Errorf("%s group_by %q: the refusal does not name the column: %v", c[0], c[1], err)
		}
		if bootUI(t, bp).CheckChart(c[0], c[1]) == nil {
			t.Errorf("%s group_by %q passes the boot check, so the load refuses a chart that works", c[0], c[1])
		}
	}
}
