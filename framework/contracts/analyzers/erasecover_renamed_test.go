package analyzers_test

import (
	"strings"
	"testing"
)

// 946fccf75, reduced: the jobs table defaults to "jobs" and a WithTable
// option renames it. The init()-time registration under the canonical
// literal covered the default, and the literal default carried on the
// field let it cover the DDL that spells the field too, so a queue
// built WithTable("billing_jobs") sat outside both planes and the rule
// stayed quiet. A field also written from a parameter needs a
// registration that resolves to the field itself.
const ecRenamedQueue = `package jobq

` + ecImports + `
type Queue struct {
	db    interface{ Exec(string, ...any) }
	table string
}

type Option func(*Queue)

func WithTable(name string) Option {
	return func(q *Queue) { q.table = name }
}

func New(db interface{ Exec(string, ...any) }, opts ...Option) *Queue {
	q := &Queue{db: db, table: "jobs"}
	for _, o := range opts {
		o(q)
	}
	q.db.Exec(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, user_id TEXT, payload TEXT)", q.qt()))
	NEWCALL
	return q
}

func (q *Queue) qt() string { return quoteFolded(q.table) }

func quoteFolded(s string) string { return "\"" + s + "\"" }
`

const ecLiteralRegistration = `package jobq

` + ecImports + `
func init() {
	datexport.Register(datexport.DataExporter{Name: "jobs", Table: "jobs"})
	datexport.RegisterEraser(datexport.DataEraser{Name: "jobs", Table: "jobs", Column: "user_id"})
}
`

func TestRenamedTableNeedsItsOwnRegistration(t *testing.T) {
	// Pre-fix: init registers the literal default only.
	ds := fixture(t, map[string]string{
		"battery/jobq/queue.go":  strings.Replace(ecRenamedQueue, "NEWCALL", "", 1),
		"battery/jobq/export.go": ecLiteralRegistration,
	})
	hs := hits1415(ds)
	if len(hs) != 1 || hs[0].File != "battery/jobq/queue.go" || hs[0].Line != 28 {
		t.Fatalf("want the renamable jobs table reported at queue.go:28, got %v", hs)
	}
	if !strings.Contains(hs[0].Message, "DataEraser") || !strings.Contains(hs[0].Message, "DataExporter") {
		t.Errorf("both planes cover only the default; message should name both: %s", hs[0].Message)
	}

	// The fix: the constructor registers the table it was built with,
	// through a helper init also calls with the default.
	ds = fixture(t, map[string]string{
		"battery/jobq/queue.go": strings.Replace(ecRenamedQueue, "NEWCALL", "registerJobs(q.table)", 1),
		"battery/jobq/export.go": `package jobq

` + ecImports + `
func init() { registerJobs("jobs") }

func registerJobs(table string) {
	datexport.Register(datexport.DataExporter{Name: "jobs", Table: table})
	datexport.RegisterEraser(datexport.DataEraser{Name: "jobs", Table: table, Column: "user_id"})
}
`,
	})
	if hs := hits1415(ds); len(hs) != 0 {
		t.Fatalf("the constructor registers q.table itself; want quiet, got %v", hs)
	}
}

// Half a fix still reports: the exporter follows the configured table,
// the eraser stays on the literal default.
func TestRenamedTableErasedOnlyUnderDefault(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/jobq/queue.go": strings.Replace(ecRenamedQueue, "NEWCALL",
			`datexport.Register(datexport.DataExporter{Name: "jobs", Table: q.table})`, 1),
		"battery/jobq/export.go": `package jobq

` + ecImports + `
func init() {
	datexport.RegisterEraser(datexport.DataEraser{Name: "jobs", Table: "jobs", Column: "user_id"})
}
`,
	})
	hs := hits1415(ds)
	if len(hs) != 1 || !strings.Contains(hs[0].Message, "DataEraser") || strings.Contains(hs[0].Message, "DataExporter (") {
		t.Fatalf("want one finding naming only the eraser plane, got %v", hs)
	}
}

// Near-miss: a field whose only writes are literals (an option that
// picks a fixed legacy name, no parameter anywhere) names tables the
// package knows, and the literal registrations still cover it.
func TestLiteralOnlyFieldKeepsLiteralCoverage(t *testing.T) {
	ds := fixture(t, map[string]string{
		"battery/jobq/queue.go": strings.NewReplacer(
			"NEWCALL", "",
			"func WithTable(name string) Option {\n\treturn func(q *Queue) { q.table = name }\n}",
			"func WithLegacyTable() Option {\n\treturn func(q *Queue) { q.table = \"jobs\" }\n}",
		).Replace(ecRenamedQueue),
		"battery/jobq/export.go": ecLiteralRegistration,
	})
	if hs := hits1415(ds); len(hs) != 0 {
		t.Fatalf("no parameter reaches the field; the literal registration covers it, got %v", hs)
	}
}
