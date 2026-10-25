package appgen

import (
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
	"github.com/chromedp/chromedp"
)

// TestJSSDKCompositeKeys runs the generated client.js in Chrome against a
// live app whose entity is keyed on two columns. Every record method takes
// the key as an array, and batchDelete builds each record id itself: one
// key value holds every printable ASCII byte and some non-ASCII, so a
// spelling that drifts from Go's url.PathEscape is refused by the server.
// A second entity keyed on one int column takes a number key, which
// batchDelete must send as the string the server reads.
func TestJSSDKCompositeKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	states := &framework.StatesConfig{
		Field:       "status",
		Initial:     []string{"enrolled"},
		Transitions: []framework.Transition{{Key: "finish", From: []string{"enrolled"}, To: "done"}},
	}
	tsOff := false
	decl := framework.EntityDeclaration{
		Name:       "enrollments",
		Table:      "enrollments",
		PrimaryKey: []string{"student_id", "course_id"},
		Timestamps: &tsOff,
		Fields: []framework.FieldDeclaration{
			{Name: "student_id", Type: "string", Required: true},
			{Name: "course_id", Type: "string", Required: true},
			{Name: "grade", Type: "string"},
			{Name: "status", Type: "enum", Values: []string{"enrolled", "done"}, Default: "enrolled"},
		},
		States: states,
	}
	widgets := framework.EntityDeclaration{
		Name:       "widgets",
		Table:      "widgets",
		PrimaryKey: []string{"code"},
		Timestamps: &tsOff,
		Fields:     []framework.FieldDeclaration{{Name: "code", Type: "int", Required: true}},
	}
	spec, err := buildSDKSpec([]framework.EntityDeclaration{decl, widgets}, &SdkOptions{name: "keys", sdkVersion: "0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderSDKFiles(spec, []string{"js"}, false)
	if err != nil {
		t.Fatal(err)
	}
	var clientJS string
	for _, f := range files {
		if f.Name == "js/client.js" {
			clientJS = f.Content
		}
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	app.Entity("enrollments", framework.EntityConfig{
		Table:      "enrollments",
		PrimaryKey: []string{"student_id", "course_id"},
		Exposure:   &framework.ExposureConfig{Public: true},
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
			{Name: "grade", Type: schema.String},
			{Name: "status", Type: schema.Enum, Values: []string{"enrolled", "done"}, Default: "enrolled"},
		},
		States: states,
	}.WithTimestamps(false))
	app.Entity("widgets", framework.EntityConfig{
		Table:      "widgets",
		PrimaryKey: []string{"code"},
		Exposure:   &framework.ExposureConfig{Public: true},
		Fields:     []schema.Field{{Name: "code", Type: schema.Int, Required: true}},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		fmt.Fprint(w, clientJS)
	})
	mux.HandleFunc("/harness", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><html><body>harness</body></html>")
	})
	mux.Handle("/", app.Router())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	scenario := `(async () => {
  const { Client } = await import('/client.js');
  const api = new Client({ baseURL: location.origin });
  let ascii = '';
  for (let c = 0x20; c < 0x7f; c++) ascii += String.fromCharCode(c);
  const s = ascii + 'é漢', c = 'c/1';
  const r = {};
  await api.enrollments.create({ studentId: s, courseId: c, grade: 'A' });
  r.get = (await api.enrollments.get([s, c])).studentId === s;
  r.patch = (await api.enrollments.patch([s, c], { grade: 'B' })).grade;
  r.move = (await api.enrollments.finish([s, c])).status;
  r.batchUpdate = (await api.enrollments.batchUpdate([{ studentId: s, courseId: c, grade: 'C' }])).committed;
  r.afterBatch = (await api.enrollments.get([s, c])).grade;
  r.batchDelete = (await api.enrollments.batchDelete([[s, c]])).committed;
  try {
    await api.enrollments.get([s, c]);
    r.gone = 'still there';
  } catch (e) {
    r.gone = e.status;
  }
  await api.enrollments.create({ studentId: 'x', courseId: c });
  await api.enrollments.remove(['x', c]);
  r.removed = (await api.enrollments.list()).total;
  await api.widgets.create({ code: 7 });
  r.numberDelete = (await api.widgets.batchDelete([7])).committed;
  r.widgets = (await api.widgets.list()).total;
  window.__result = r;
})().catch((e) => { window.__result = { fatal: String((e && e.stack) || e) }; });
true`
	ctx := chromedptest.Context(t, chromedptest.Timeout(120*time.Second))
	var r map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate("http://"+ln.Addr().String()+"/harness"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(scenario, nil),
		chromedp.Poll(`window.__result`, &r, chromedp.WithPollingTimeout(60*time.Second)),
	); err != nil {
		t.Fatalf("browser: %v", err)
	}
	if fatal, ok := r["fatal"]; ok {
		t.Fatalf("scenario failed in the browser: %v", fatal)
	}
	for key, want := range map[string]any{
		"get": true, "patch": "B", "move": "done", "batchUpdate": true,
		"afterBatch": "C", "batchDelete": true, "gone": float64(404), "removed": float64(0),
		"numberDelete": true, "widgets": float64(0),
	} {
		if r[key] != want {
			t.Errorf("%s = %v, want %v", key, r[key], want)
		}
	}
}
