package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

const sweepPostsEntity = entityImports + "\nfunc declare(app *framework.App) {\n\tapp.Entity(\"posts\", entity.EntityConfig{\n\t\tFields: []schema.Field{{Name: \"title\", Type: schema.String}},\n\t})\n}\n"

func TestSweepCoreUIHostImportRendersUI(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile(`	r.Handle("GET", "/orders/page/{n}", nil)`),
		"page.go": "package main\n\nimport \"github.com/DonaldMurillo/gofastr/framework/uihost\"\n\nvar _ uihost.Option\n",
	})
	assertHas(t, ds, contracts.RuleStateAsRoute)
}

func TestSweepCoreImportSuffixNeedsASegmentBoundary(t *testing.T) {
	ds := fixture(t, map[string]string{
		"e.go": "package main\n\nimport \"example.com/myframework\"\n\nfunc wire(app *myframework.App) {\n\tapp.Entity(\"posts\", myframework.EntityConfig{})\n}\n",
	})
	assertNot(t, ds, contracts.RuleCrudWithoutAuth, "myframework is not the framework")
}

func TestSweepCoreLegacyAnnotationReachesFourLinesUp(t *testing.T) {
	ds := fixture(t, map[string]string{
		"entities.go": sweepPostsEntity,
		"report.go":   "package main\n\nimport \"database/sql\"\n\nfunc count(db *sql.DB) int {\n\t// SAFE-SQL: a fixed admin report\n\tvar n int\n\tq := 1\n\t_ = q\n\t_ = db.QueryRow(\"SELECT COUNT(*) FROM posts\").Scan(&n)\n\treturn n\n}\n",
	})
	assertNot(t, ds, contracts.RuleRawSQLOverRepo, "the annotation is three non-blank lines up, in any case")
}

func TestSweepCoreTwoVerbsAreNotHandrolledCRUD(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/orders\", nil)\n\tr.Handle(\"POST\", \"/orders\", nil)"),
	})
	assertNot(t, ds, contracts.RuleHandrolledCRUD, "two verbs on one resource is ordinary custom behaviour")
}

func TestSweepCoreColonParamGroupsWithItsCollection(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/orders\", nil)\n\tr.Handle(\"PUT\", \"/orders/:id\", nil)\n\tr.Handle(\"DELETE\", \"/orders/:id\", nil)"),
	})
	assertHas(t, ds, contracts.RuleHandrolledCRUD)
}

func TestSweepCoreFrameworkSurfaceIsNotAResource(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go": routerFile("\tr.Handle(\"GET\", \"/__gofastr/jobs\", nil)\n\tr.Handle(\"POST\", \"/__gofastr/jobs\", nil)\n\tr.Handle(\"DELETE\", \"/__gofastr/jobs/{id}\", nil)"),
	})
	assertNot(t, ds, contracts.RuleHandrolledCRUD, "/__gofastr is the framework's own surface")
}

const sweepSMTP = "package main\n\nimport \"net/smtp\"\n\nvar _ = smtp.SendMail\n"

func TestSweepCoreBatteryTreesAreNotHandrolled(t *testing.T) {
	ds := fixture(t, map[string]string{"battery/email/send.go": sweepSMTP})
	assertNot(t, ds, contracts.RuleHandrolledBattery, "the battery itself uses net/smtp")
}

func TestSweepCoreFileUsingTheBatteryIsComposition(t *testing.T) {
	ds := fixture(t, map[string]string{"mail.go": "package main\n\nimport (\n\t\"net/smtp\"\n\n\t\"github.com/DonaldMurillo/gofastr/battery/email\"\n)\n\nvar _ = smtp.SendMail\nvar _ email.Mailer\n"})
	assertNot(t, ds, contracts.RuleHandrolledBattery, "beside the battery it is composition")
}

func TestSweepCoreUnmountedSourceNeedsASegmentBoundary(t *testing.T) {
	out, err := codegen.Stamp("send.go", sweepSMTP, codegen.Marker{Generator: "unmount", Kind: codegen.KindScaffold, Version: "v0.91.0", Source: "github.com/DonaldMurillo/gofastr/battery/emailer"})
	if err != nil {
		t.Fatal(err)
	}
	ds := fixture(t, map[string]string{"unmounted/send.go": out})
	assertHas(t, ds, contracts.RuleHandrolledBattery)
}

func TestSweepCoreTableNameOutsideATablePositionIsClean(t *testing.T) {
	ds := fixture(t, map[string]string{
		"entities.go": sweepPostsEntity,
		"report.go":   "package main\n\nimport \"database/sql\"\n\nfunc count(db *sql.DB) { _, _ = db.Query(\"SELECT posts FROM metrics\") }\n",
	})
	assertNot(t, ds, contracts.RuleRawSQLOverRepo, "posts is a column here")
}

func TestSweepCoreReportingQueryMarkerIsHonoured(t *testing.T) {
	ds := fixture(t, map[string]string{
		"entities.go": sweepPostsEntity,
		"report.go":   "package main\n\nimport \"database/sql\"\n\nfunc count(db *sql.DB) {\n\t// reporting-query: an admin count\n\t_, _ = db.Query(\"SELECT COUNT(*) FROM posts\")\n}\n",
	})
	assertNot(t, ds, contracts.RuleRawSQLOverRepo, "reporting-query: is the second escape hatch")
}

func TestSweepCoreLegacyAnnotationStopsAtAFunction(t *testing.T) {
	ds := fixture(t, map[string]string{
		"entities.go": sweepPostsEntity,
		"report.go":   "package main\n\nimport \"database/sql\"\n\nfunc a() {\n\t// safe-sql: belongs to a\n}\n\nfunc f(db *sql.DB) { _, _ = db.Query(\"SELECT * FROM posts\") }\n",
	})
	assertHas(t, ds, contracts.RuleRawSQLOverRepo)
}
