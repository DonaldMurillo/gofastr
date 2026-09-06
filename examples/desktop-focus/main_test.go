package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The app in its `--serve` shape (no Run, no window):
// framework.TestHarness drives the REST routes and the screens through
// the real router, with the desktop battery registered, exactly what
// `--serve :8080` serves. GOFASTR_DESKTOP_DATA_DIR points the data dir
// at a temp dir so the suite never touches the developer's Application
// Support.

// harnessUser is the test harness's request identity: GetID feeds the
// owner extractor the same way the desktop local identity does.
type harnessUser struct{ id string }

func (u harnessUser) GetID() string      { return u.id }
func (u harnessUser) GetEmail() string   { return "local@" + u.id }
func (u harnessUser) GetRoles() []string { return nil }

// newTestApp builds the app without Run and migrates (the harness
// never Starts, so the tables are created the way examples/blog does
// before seeding).
func newTestApp(t *testing.T) (*framework.App, *desktop.Battery, *desktoptest.Shell, *Engine) {
	t.Helper()
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	shell := desktoptest.NewShell()
	app, d, eng, err := buildApp(shell)
	if err != nil {
		t.Fatal(err)
	}
	if err := framework.AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatal(err)
	}
	return app, d, shell, eng
}

// TestOwnerScopingAcrossUsers: user u1's task and session are invisible
// to u2 on the API and the screens (hard rule 6).
func TestOwnerScopingAcrossUsers(t *testing.T) {
	app, _, _, _ := newTestApp(t)
	u1 := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})
	u2 := framework.TestHarness(t, app).AsUser(harnessUser{id: "u2"})

	created := u1.Post("/api/tasks", map[string]any{"title": "u1 secret task"}).AssertStatus(t, http.StatusCreated)
	var row map[string]any
	if err := decodeData(created.Body(), &row); err != nil {
		t.Fatal(err)
	}
	id, _ := row["id"].(string)
	if owner, _ := row["userId"].(string); owner != "u1" {
		t.Fatalf("owner = %q, want u1 (stamped on create)", owner)
	}
	u1.Post("/api/sessions", map[string]any{"kind": "work", "minutes": 25, "started_at": "2026-09-05T09:00:00Z", "ends_at": "2026-09-05T09:25:00Z"}).AssertStatus(t, http.StatusCreated)

	// The API hides both from u2.
	if body := u2.Get("/api/tasks").AssertStatus(t, http.StatusOK).Body(); strings.Contains(body, "u1 secret task") {
		t.Fatal("u2's task list contains u1's task")
	}
	if body := u2.Get("/api/sessions").AssertStatus(t, http.StatusOK).Body(); strings.Contains(body, `"minutes"`) {
		t.Fatal("u2's session list contains u1's session")
	}
	// So does the dashboard, and a foreign detail id renders "Not
	// found", never another user's title.
	if body := u2.Get("/").AssertStatus(t, http.StatusOK).Body(); strings.Contains(body, "u1 secret task") {
		t.Fatal("u2's dashboard contains u1's task")
	}
	if body := u2.Get("/tasks/"+id).AssertStatus(t, http.StatusOK).Body(); strings.Contains(body, "u1 secret task") {
		t.Fatal("u2's task detail leaked u1's title")
	}
	// u1 still sees it everywhere.
	if body := u1.Get("/api/tasks").AssertStatus(t, http.StatusOK).Body(); !strings.Contains(body, "u1 secret task") {
		t.Fatal("u1's own list lost the task")
	}
}

// TestEveryScreenRendersDesignSystemMarkup: each screen answers 200
// with design-system markup and no inline style or script.
func TestEveryScreenRendersDesignSystemMarkup(t *testing.T) {
	app, _, _, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	task := ta.Post("/api/tasks", map[string]any{"title": "Markup probe"}).AssertStatus(t, http.StatusCreated)
	var row map[string]any
	if err := decodeData(task.Body(), &row); err != nil {
		t.Fatal(err)
	}
	id, _ := row["id"].(string)

	screens := []struct{ path, want string }{
		{"/", "data-fui-comp"},
		{"/tasks", "Markup probe"},
		{"/tasks/new", "New Task"},
		{"/tasks/" + id, "Markup probe"},
		{"/tasks/" + id + "/edit", "Markup probe"},
		{"/history", "No sessions yet"},
		{"/settings", "Work minutes"},
		{"/widget", "data-fui-window-drag"},
	}
	for _, s := range screens {
		body := ta.Get(s.path).AssertStatus(t, http.StatusOK).Body()
		if !strings.Contains(body, s.want) {
			t.Fatalf("%s missing %q", s.path, s.want)
		}
		if strings.Contains(body, "<style") || strings.Contains(body, "style=\"") {
			t.Fatalf("%s carries inline CSS", s.path)
		}
		if strings.Contains(body, "<script>") {
			t.Fatalf("%s carries an inline script", s.path)
		}
	}

	// The external script is referenced by its hash-versioned URL,
	// never inlined, and the embedded route serves it.
	home := ta.Get("/").AssertStatus(t, http.StatusOK).Body()
	if !strings.Contains(home, `src="/desktop-focus.js?v=`) {
		t.Fatal("external page script not referenced")
	}
	js := ta.Get("/desktop-focus.js").AssertStatus(t, http.StatusOK)
	if !strings.Contains(js.Body(), "data-focus-countdown") {
		t.Fatalf("page script route served unexpected bytes: %.80s", js.Body())
	}

	// The dashboard carries the timer hooks the page script wires.
	for _, want := range []string{
		`data-focus-countdown`, `data-focus-action="start"`, `data-focus-action="pause"`,
		`data-focus-action="resume"`, `data-focus-action="skip"`, `data-focus-start="` + id + `"`,
		"Focused today", "Sessions today", "Tasks done",
	} {
		if !strings.Contains(home, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
	// The widget carries its own countdown and controls.
	widget := ta.Get("/widget").AssertStatus(t, http.StatusOK).Body()
	for _, want := range []string{`data-focus-countdown`, `data-focus-action="start"`, `data-focus-widget-close`, "Timer"} {
		if !strings.Contains(widget, want) {
			t.Fatalf("widget missing %q", widget[:0])
		}
	}
}

// TestSettingsScreenRendersDeclaredPreferences: /settings is the
// battery's preferences form, one field per declaration, prefilled
// with the declared defaults (this is the --serve shape: no Run, no
// app state store, so the defaults are the answer).
func TestSettingsScreenRendersDeclaredPreferences(t *testing.T) {
	app, _, _, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	body := ta.Get("/settings").AssertStatus(t, http.StatusOK).Body()
	for _, want := range []string{
		`id="f-work_minutes"`, `value="25"`, `min="1"`, `max="180"`,
		`id="f-break_minutes"`, `value="5"`,
		`id="f-notify_on_done"`, `id="f-tray_countdown"`,
		`id="f-sound"`, `<option selected="selected" value="chime"`,
		`data-fui-rpc="/__gofastr/desktop/preferences"`,
		"Work minutes", "Notify when a session ends", "Session sound",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/settings missing %q", want)
		}
	}
	if strings.Contains(body, "<style") || strings.Contains(body, `style="`) {
		t.Fatal("the settings screen ships CSS")
	}
}

// decodeData unwraps the CRUD envelope's {"data": …} object, the shape
// create responses use (camelCase keys inside).
func decodeData(body string, dest *map[string]any) error {
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return err
	}
	*dest = envelope.Data
	return nil
}
