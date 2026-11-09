package appgen

// statCheckProbe is written into the union app by TestUnionApp. The
// dashboard's stat cards filter invoices on status; an invoices entity
// with no status column is an app whose cards could never compute, the
// shape a spec load passes through (a filter naming an unknown column)
// and entityui.StatValue draws as "—" forever. Mounting the generated
// screen must refuse it through entityui.CheckStat, naming the screen
// and the card, before the screen registers (#669).
//
// TestChartCheckedAtBoot is the chart twin: the cards screen draws a
// pie_chart grouped on "group", and a cards entity with no group column
// would draw an empty chart forever. Mounting the screen must refuse it
// through entityui.CheckChart, naming the screen and the chart.
const statCheckProbe = `package main

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestStatCardCheckedAtBoot(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fwApp := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	fwApp.Entity("invoices", entity.EntityConfig{
		Table:  "invoices",
		Fields: []schema.Field{{Name: "amount", Type: schema.Decimal}},
	})
	saved := appUI
	t.Cleanup(func() { appUI = saved })
	appUI = fwApp.EntityUI(appExtensions)

	got := func() (r any) {
		defer func() { r = recover() }()
		mountDashboardScreen(fwApp, nil, db)
		return nil
	}()
	if got == nil {
		t.Fatal("mountDashboardScreen mounted stat cards over a column the entity lacks")
	}
	msg := fmt.Sprint(got)
	for _, want := range []string{` + "`" + `screen "dashboard"` + "`" + `, ` + "`" + `stat_card "Billed"` + "`" + `, "status"} {
		if !strings.Contains(msg, want) {
			t.Errorf("boot refusal %q does not name %s", msg, want)
		}
	}
}

func TestChartCheckedAtBoot(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fwApp := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	fwApp.Entity("cards", entity.EntityConfig{
		Table: "cards",
		Fields: []schema.Field{
			{Name: "label", Type: schema.String},
			{Name: "order", Type: schema.Int},
		},
	})
	saved := appUI
	t.Cleanup(func() { appUI = saved })
	appUI = fwApp.EntityUI(appExtensions)

	got := func() (r any) {
		defer func() { r = recover() }()
		mountCardsScreen(fwApp, nil, db)
		return nil
	}()
	if got == nil {
		t.Fatal("mountCardsScreen mounted a chart grouped on a column the entity lacks")
	}
	msg := fmt.Sprint(got)
	for _, want := range []string{` + "`" + `screen "cards"` + "`" + `, ` + "`" + `pie_chart "Cards by group"` + "`" + `, "group"} {
		if !strings.Contains(msg, want) {
			t.Errorf("boot refusal %q does not name %s", msg, want)
		}
	}
}
`
