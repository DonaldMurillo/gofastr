package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/desktoptest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// The app is exercised in its `--serve` shape (no Run, no window):
// TestHarness drives the REST routes and the screens through the real
// router, with the desktop battery registered, exactly what
// `--serve :8080` serves. GOFASTR_DESKTOP_DATA_DIR points the data dir
// at a temp dir so the suite never touches the developer's Application
// Support.

type harnessUser struct{ id string }

func (u harnessUser) GetID() string { return u.id }
func newTestApp(t *testing.T) (*framework.App, *desktop.Battery, *desktoptest.Shell) {
	t.Helper()
	t.Setenv("GOFASTR_DESKTOP_DATA_DIR", t.TempDir())
	shell := desktoptest.NewShell()
	app, d, err := buildApp(shell)
	if err != nil {
		t.Fatal(err)
	}
	// app.Start runs migrations; the harness never Starts, so create
	// the tables the way examples/blog does before seeding.
	if err := framework.AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatal(err)
	}
	return app, d, shell
}

// TestOwnerScopingAcrossUsers: user u1's note is invisible to u2 on
// both the API and the list screen (hard rule 6).
func TestOwnerScopingAcrossUsers(t *testing.T) {
	app, _, _ := newTestApp(t)
	u1 := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})
	u2 := framework.TestHarness(t, app).AsUser(harnessUser{id: "u2"})
	created := u1.Post("/api/notes", map[string]any{"title": "u1 secret", "body": "mine"}).AssertStatus(t, http.StatusCreated)
	var row map[string]any
	if err := decodeData(created.Body(), &row); err != nil {
		t.Fatal(err)
	}
	id, _ := row["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %s", created.Body())
	}
	if owner, _ := row["userId"].(string); owner != "u1" {
		t.Fatalf("owner = %q, want u1 (stamped on create)", owner)
	}

	// The API hides it from u2.
	u2List := u2.Get("/api/notes").AssertStatus(t, http.StatusOK)
	if strings.Contains(u2List.Body(), "u1 secret") {
		t.Fatal("u2's list contains u1's note")
	}
	// So does the list screen.
	u2Screen := u2.Get("/").AssertStatus(t, http.StatusOK)
	if strings.Contains(u2Screen.Body(), "u1 secret") {
		t.Fatal("u2's list screen contains u1's note")
	}
	// And the editor Load falls back rather than leaking the title.
	u2Edit := u2.Get("/notes/"+id).AssertStatus(t, http.StatusOK)
	if strings.Contains(u2Edit.Body(), "u1 secret") {
		t.Fatal("u2's editor screen contains u1's note title")
	}

	// u1 still sees it everywhere.
	u1List := u1.Get("/api/notes").AssertStatus(t, http.StatusOK)
	if !strings.Contains(u1List.Body(), "u1 secret") {
		t.Fatal("u1's own list lost the note")
	}
}

// TestSearchSpansTitleAndBody: ?q= matches body text too (SearchFields
// on title and body), through the API and the screen's search box.
func TestSearchSpansTitleAndBody(t *testing.T) {
	app, _, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	ta.Post("/api/notes", map[string]any{"title": "Plain title", "body": "the needle is here"}).AssertStatus(t, http.StatusCreated)

	api := ta.Get("/api/notes?q=needle").AssertStatus(t, http.StatusOK)
	if !strings.Contains(api.Body(), "Plain title") {
		t.Fatalf("?q=needle on the API missed the body match: %s", api.Body())
	}
	screen := ta.Get("/notes?q=needle").AssertStatus(t, http.StatusOK)
	if !strings.Contains(screen.Body(), "Plain title") {
		t.Fatal("the list screen's search missed the body match")
	}
}

// TestScreensRender: the three screens answer with the design system's
// markup and the copy-link hook.
func TestScreensRender(t *testing.T) {
	app, _, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	home := ta.Get("/").AssertStatus(t, http.StatusOK)
	for _, want := range []string{"Notes", "New Note", "search", "data-fui"} {
		if !strings.Contains(home.Body(), want) {
			t.Fatalf("/ missing %q", want)
		}
	}
	if strings.Contains(home.Body(), "<style") {
		t.Fatal("bespoke inline CSS on the list screen")
	}

	editor := ta.Get("/notes/new").AssertStatus(t, http.StatusOK)
	for _, want := range []string{"New Note", "Copy link", "data-notes-copy", "form"} {
		if !strings.Contains(editor.Body(), want) {
			t.Fatalf("/notes/new missing %q", want)
		}
	}
	if strings.Contains(editor.Body(), "<script>") {
		t.Fatal("inline script on the editor screen")
	}

	// The external script is referenced by its hash-versioned URL, never
	// inlined, and the embedded route serves it (a cwd-relative static
	// dir would 404 from a Finder-launched bundle).
	if !strings.Contains(home.Body(), `src="/desktop-notes.js?v=`) {
		t.Fatal("external page script not referenced")
	}
	js := ta.Get("/desktop-notes.js").AssertStatus(t, http.StatusOK)
	if !strings.Contains(js.Body(), "desktop") {
		t.Fatalf("page script route served unexpected bytes: %.80s", js.Body())
	}
}

// TestListIslandEndpointServesTable: the sort/pagination RPC the list
// island fires at /api/tables/answers with the same table HTML the
// screen painted (GOFASTR1003's route now has a test). The harness
// sets no user, so these requests carry the desktop battery's local
// identity: the single-installation user every anonymous request
// falls back to, and the owner the rows are scoped to.
func TestListIslandEndpointServesTable(t *testing.T) {
	app, _, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "local"})

	ta.Post("/api/notes", map[string]any{"title": "Island row", "body": ""}).AssertStatus(t, http.StatusCreated)

	frag := ta.Get("/api/tables/notes").AssertStatus(t, http.StatusOK)
	if !strings.Contains(frag.Body(), "Island row") {
		t.Fatalf("island fragment missing the row: %.200s", frag.Body())
	}
	screen := ta.Get("/").AssertStatus(t, http.StatusOK)
	if !strings.Contains(screen.Body(), "Island row") {
		t.Fatal("list screen missing the row the harness user owns")
	}
}

// TestSaveRoundTripThroughFormEndpoint: the editor saves through the
// entity's REST route and the updated note renders.
func TestSaveRoundTripThroughFormEndpoint(t *testing.T) {
	app, _, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	created := ta.Post("/api/notes", map[string]any{"title": "First", "body": "hello"}).AssertStatus(t, http.StatusCreated)
	var row map[string]any
	if err := decodeData(created.Body(), &row); err != nil {
		t.Fatal(err)
	}
	id, _ := row["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %s", created.Body())
	}

	ta.Put("/api/notes/"+id, map[string]any{"title": "First, edited", "body": "hello"}).AssertStatus(t, http.StatusOK)

	detail := ta.Get("/notes/"+id).AssertStatus(t, http.StatusOK)
	if !strings.Contains(detail.Body(), "First, edited") {
		t.Fatal("detail screen does not reflect the saved title")
	}
	// The window title follows the open note: ScreenTitle feeds
	// document.title.
	if !strings.Contains(detail.Body(), "<title>First, edited") {
		t.Fatalf("page title does not follow the note: %.200s", detail.Body())
	}
	// The detail page carries the engine's Edit action, and the editor
	// lives at /{id}/edit so its Cancel (BasePath/{id}) lands on the
	// detail page instead of on itself.
	if !strings.Contains(detail.Body(), `href="/notes/`+id+`/edit"`) {
		t.Fatal("detail screen has no Edit action")
	}
	editScreen := ta.Get("/notes/"+id+"/edit").AssertStatus(t, http.StatusOK)
	if !strings.Contains(editScreen.Body(), "First, edited") {
		t.Fatal("editor screen does not prefill the saved title")
	}
	if !strings.Contains(editScreen.Body(), `href="/notes/`+id+`"`) {
		t.Fatal("editor Cancel does not point at the detail page")
	}
}

// TestSettingsScreenRendersDeclaredPreferences: /settings is the
// battery's preferences form, one field per declaration, prefilled
// with the declared defaults (this is the --serve shape: no Run, no
// app state store, so the defaults are the answer and there is no row
// and no /settings/{id}).
func TestSettingsScreenRendersDeclaredPreferences(t *testing.T) {
	app, d, _ := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	screen := ta.Get("/settings").AssertStatus(t, http.StatusOK)
	for _, want := range []string{
		"Settings", `id="f-notify_on_save"`, `id="f-export_folder"`,
		"Notify on save", "Export folder", `data-fui-rpc="/__gofastr/desktop/preferences"`,
	} {
		if !strings.Contains(screen.Body(), want) {
			t.Fatalf("/settings missing %q: %.300s", want, screen.Body())
		}
	}
	// The default renders the box checked (the renderer emits the
	// attributes in sorted order, checked before id).
	if !strings.Contains(screen.Body(), `<input checked="checked" id="f-notify_on_save"`) {
		t.Fatal("the default notify_on_save does not render checked")
	}
	// The typed read answers the default before any store exists.
	if !d.Preferences().Bool("notify_on_save") {
		t.Fatal("notify_on_save default is not true")
	}
	// The old settings entity and its routes are gone.
	ta.Get("/api/settings").AssertStatus(t, http.StatusNotFound)
}

// TestSaveNotifiesOnTheDeclaredDefault: in the --serve shape there is
// no app state store, so notify_on_save reads its declared default and
// every save notifies. Turning it off needs the desktop host; the
// harness suite covers that direction through preferences.set.
func TestSaveNotifiesOnTheDeclaredDefault(t *testing.T) {
	app, _, shell := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	ta.Post("/api/notes", map[string]any{"title": "Default on", "body": ""}).AssertStatus(t, http.StatusCreated)
	ta.Post("/api/notes", map[string]any{"title": "Still on", "body": ""}).AssertStatus(t, http.StatusCreated)
	if n := len(shell.Notifications()); n != 2 {
		t.Fatalf("notifications on the declared default = %d, want 2", n)
	}
}

// TestSavedNoteFiresNotification: the AfterCreate hook reaches the
// shell's notifier through the fake shell (the plugin and battery
// wiring, observable without a window).
func TestSavedNoteFiresNotification(t *testing.T) {
	app, _, shell := newTestApp(t)
	ta := framework.TestHarness(t, app).AsUser(harnessUser{id: "u1"})

	ta.Post("/api/notes", map[string]any{"title": "Notify me", "body": ""}).AssertStatus(t, http.StatusCreated)

	notes := shell.Notifications()
	if len(notes) != 1 {
		t.Fatalf("notifications = %d, want 1", len(notes))
	}
	if notes[0].Title != "Saved" || notes[0].Body != "Notify me" {
		t.Fatalf("notification = %+v", notes[0])
	}
}

// TestManifestIncludesPluginCapability: Run in manifest mode (what
// `gofastr desktop types` drives) freezes and prints the manifest,
// which must contain the plugin-registered systeminfo capability
// alongside the core ones.
func TestManifestIncludesPluginCapability(t *testing.T) {
	app, d, shell := newTestApp(t)
	// InitPlugins is what Start (and a harness) run; the manifest must
	// reflect the plugin capability registered from its Init.
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(desktop.ManifestEnv, "1")

	var out string
	func() {
		old := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		var buf strings.Builder
		done := make(chan struct{})
		go func() { defer close(done); _, _ = io.Copy(&buf, r) }()
		func() {
			defer func() { os.Stdout = old }()
			if err := d.Run(app); err != nil {
				t.Errorf("Run in manifest mode: %v", err)
			}
			w.Close()
		}()
		<-done
		r.Close()
		out = buf.String()
	}()

	for _, want := range []string{`"systeminfo"`, `"cpuCount"`, `"clipboard"`, `"window"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("manifest missing %s:\n%s", want, out)
		}
	}
	if _, ran := shell.Config(); ran {
		t.Fatal("manifest mode touched the shell")
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
