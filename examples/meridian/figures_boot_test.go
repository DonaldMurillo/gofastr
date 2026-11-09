package main

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/entityui"

	"github.com/DonaldMurillo/gofastr/examples/meridian/entities"
)

// bootFigures runs RegisterGenerated, the boot path that builds appUI
// and mounts every screen, over the real registry on an in-memory
// database, and returns what it panicked with (nil for a clean boot).
func bootFigures(t *testing.T) (r any) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fwApp := framework.NewApp(framework.WithDB(db))
	entities.RegisterAll(fwApp)
	saved := appUI
	t.Cleanup(func() { appUI = saved })
	defer func() { r = recover() }()
	RegisterGenerated(fwApp, nil, db)
	return nil
}

// Every stat and chart Meridian draws with a hand-written reader passes
// entityui's boot checks: the checks refuse nothing that computes.
func TestFiguresPassBootCheck(t *testing.T) {
	if r := bootFigures(t); r != nil {
		t.Fatalf("Meridian's own figures stopped the boot: %v", r)
	}
}

// A hand-written figure that could never compute stops the boot, named
// by its surface and label, on the Revenue page and the Billing panel.
func TestBrokenFigureStopsBoot(t *testing.T) {
	cases := []struct {
		name  string
		brk   func() (restore func())
		wants []string
	}{
		{"revenue stat", func() func() {
			saved := revenueStats
			revenueStats = slices.Clone(saved)
			revenueStats[1].field = "total"
			return func() { revenueStats = saved }
		}, []string{`admin page "revenue"`, `stat "Collected"`, "total"}},
		{"revenue chart", func() func() {
			saved := revenueCharts
			revenueCharts = slices.Clone(saved)
			revenueCharts[1].groupBy = "channel"
			return func() { revenueCharts = saved }
		}, []string{`admin page "revenue"`, `chart "Payments by method"`, "channel"}},
		{"billing figure", func() func() {
			saved := billingFigures
			billingFigures = slices.Clone(saved)
			billingFigures[0].where = `state = "open"`
			return func() { billingFigures = saved }
		}, []string{`customers side panel "billing"`, `stat "Outstanding"`, "state"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer c.brk()()
			r := bootFigures(t)
			if r == nil {
				t.Fatal("Meridian booted with a figure that could never compute")
			}
			msg := fmt.Sprint(r)
			for _, want := range c.wants {
				if !strings.Contains(msg, want) {
					t.Errorf("boot refusal %q does not name %s", msg, want)
				}
			}
		})
	}
}

// The Overview's mount checks its stat cards and chart against the
// entity the app registered: a customers entity with no status column,
// which both the Active customers card and the chart read, stops the
// mount naming the screen, the card and the column.
func TestDashboardStatCheckedAtBoot(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	fwApp := framework.NewApp(framework.WithDB(db))
	text := func(names ...string) []schema.Field {
		out := make([]schema.Field, len(names))
		for i, n := range names {
			out[i] = schema.Field{Name: n, Type: schema.String}
		}
		return out
	}
	fwApp.Entity("subscriptions", entity.EntityConfig{Table: "subscriptions", Fields: append(text("status"), schema.Field{Name: "mrr", Type: schema.Decimal})})
	fwApp.Entity("customers", entity.EntityConfig{Table: "customers", Fields: text("name")})
	fwApp.Entity("invoices", entity.EntityConfig{Table: "invoices", Fields: text("status")})
	fwApp.Entity("plans", entity.EntityConfig{Table: "plans", Fields: text("name")})
	saved := appUI
	t.Cleanup(func() { appUI = saved })
	appUI = fwApp.EntityUI(entityui.Extensions{})

	got := func() (r any) {
		defer func() { r = recover() }()
		mountDashboardScreen(fwApp, nil, db)
		return nil
	}()
	if got == nil {
		t.Fatal("mountDashboardScreen mounted cards over a column the entity lacks")
	}
	msg := fmt.Sprint(got)
	for _, want := range []string{`screen "dashboard"`, `stat_card "Active customers"`, "status"} {
		if !strings.Contains(msg, want) {
			t.Errorf("boot refusal %q does not name %s", msg, want)
		}
	}
}
