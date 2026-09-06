package main

import (
	"context"
	"fmt"
	"net/http"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/resource"
)

// The screens. All markup comes from framework/ui components through
// framework/ui/resource's List/Form engine: the example ships zero CSS
// and zero hand-rolled structural markup (hard rules 7 and 8).

// notesListResource is the resource engine config behind the list
// screen. BasePath is /notes (row links, the New button, and the
// search form target); the list screen itself is also registered at
// "/" so the window opens on it.
func notesListResource(app *framework.App) resource.Config {
	return resource.Config{
		Entity:   "notes",
		Title:    "Notes",
		Singular: "Note",
		BasePath: "/notes",
		APIPath:  "/api/notes",
		Crud:     searchAcrossFields{src: app.MustCrudHandler("notes")},
		Fields: []resource.Field{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "updated_at", Label: "Updated", Type: "timestamp"},
		},
	}.WithSearch("title").WithCreate().WithEdit().WithIsland("/api/tables/notes")
}

// notesFormResource is the editor's config: only the fields the form
// edits. The submit goes through the runtime's form intercept to the
// entity's REST route (POST /api/notes, PUT /api/notes/{id}).
func notesFormResource(src resource.DataSource) resource.Config {
	return resource.Config{
		Entity:   "notes",
		Title:    "Notes",
		Singular: "Note",
		BasePath: "/notes",
		APIPath:  "/api/notes",
		Crud:     src,
		Fields: []resource.Field{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "body", Label: "Body", Type: "text"},
		},
	}
}

// notesListScreen is "/" (and "/notes", the search form's target): the
// notes table with its search box, New button, and island
// sort/pagination.
type notesListScreen struct {
	component.ContextOnly
	res resource.Config
}

func (s *notesListScreen) ScreenTitle() string       { return "Notes" }
func (s *notesListScreen) ScreenDescription() string { return "Local-first notes" }

func (s *notesListScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.res.List(ctx)
}

// noteDetailScreen is "/notes/{id}": the resource engine's detail view
// with its Edit and Delete actions. It exists because the engine's
// edit form points its Cancel (and its post-save navigation) at
// BasePath/{id}; registering the editor there made Cancel land on the
// page it was already on, which read as a dead button.
type noteDetailScreen struct {
	component.ContextOnly
	res   resource.Config
	id    string
	title string
}

func (s *noteDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *noteDetailScreen) Load(ctx context.Context) error {
	s.title = "Note"
	row, err := s.res.Crud.GetOne(ctx, s.id, nil)
	if err != nil || row == nil {
		return nil
	}
	if v, present := row["title"]; present {
		t, ok := v.(string)
		if !ok {
			return fmt.Errorf("desktop-notes: title column is %T, want string", v)
		}
		if t != "" {
			s.title = t
		}
	}
	return nil
}

func (s *noteDetailScreen) ScreenTitle() string       { return s.title }
func (s *noteDetailScreen) ScreenDescription() string { return "A note" }

func (s *noteDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.res.Detail(ctx, s.id)
}

// noteEditorScreen is "/notes/new" (create) and "/notes/{id}/edit" (edit).
// Load resolves the open note's title so the page <title>, and through
// the page script the window title, follows the note; the lookup goes
// through the owner-scoped DataSource, so a foreign id renders "Not
// found", never another user's title.
type noteEditorScreen struct {
	component.ContextOnly
	src   resource.DataSource
	id    string
	title string
}

func (s *noteEditorScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *noteEditorScreen) Load(ctx context.Context) error {
	if s.id == "" {
		s.title = "New note"
		return nil
	}
	row, err := s.src.GetOne(ctx, s.id, nil)
	s.title = "Note"
	if err != nil || row == nil {
		return nil
	}
	if v, present := row["title"]; present {
		t, ok := v.(string)
		if !ok {
			return fmt.Errorf("desktop-notes: title column is %T, want string", v)
		}
		if t != "" {
			s.title = t
		}
	}
	return nil
}

func (s *noteEditorScreen) ScreenTitle() string       { return s.title }
func (s *noteEditorScreen) ScreenDescription() string { return "Edit a note" }

func (s *noteEditorScreen) RenderCtx(ctx context.Context) render.HTML {
	form := notesFormResource(s.src).Form(ctx, s.id)
	// Copy link rides along on a plain Button with a data attribute;
	// static/desktop-notes.js owns the click. The attribute cannot
	// spoof runtime wiring (SafeCarrierAttrs drops data-fui-*).
	copyLink := ui.Button(ui.ButtonConfig{
		Label:      "Copy link",
		Variant:    ui.ButtonSecondary,
		ExtraAttrs: map[string]string{"data-notes-copy": ""},
	})
	return render.Join(form, ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, copyLink))
}

// settingsResource is the settings form's config. BasePath is
// /settings; the engine's post-save target /settings/{id} is also a
// registered screen (the same landing trick as the notes editor).
func settingsResource(app *framework.App) resource.Config {
	return resource.Config{
		Entity:   "settings",
		Title:    "Settings",
		Singular: "Settings",
		BasePath: "/settings",
		APIPath:  "/api/settings",
		Crud:     app.MustCrudHandler("settings"),
		Fields: []resource.Field{
			{Key: "notify_on_save", Label: "Notify on save", Type: "bool"},
			{Key: "export_folder", Label: "Export folder", Type: "string"},
		},
	}
}

// settingsScreen is "/settings" (and "/settings/{id}", the form's
// post-save target): the owner's one settings row as the resource
// engine's form. The first visit creates the row with the declared
// defaults.
type settingsScreen struct {
	component.ContextOnly
	res  resource.Config
	crud *crud.CrudHandler
	id   string
}

func (s *settingsScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *settingsScreen) Load(ctx context.Context) error {
	if s.id != "" {
		return nil
	}
	rows, err := s.res.Crud.ListAll(ctx, crud.ListOptions{Limit: 1})
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		if v, present := rows[0]["id"]; present {
			s.id, _ = v.(string)
		}
		return nil
	}
	created, err := s.crud.CreateOne(ctx, map[string]any{
		"notify_on_save": true,
		"export_folder":  "",
	})
	if err != nil {
		return err
	}
	if v, present := created["id"]; present {
		s.id, _ = v.(string)
	}
	return nil
}

func (s *settingsScreen) ScreenTitle() string       { return "Settings" }
func (s *settingsScreen) ScreenDescription() string { return "Preferences for this installation" }

func (s *settingsScreen) RenderCtx(ctx context.Context) render.HTML {
	if s.id == "" {
		return render.Tag("p", nil, render.Text("Settings row unavailable."))
	}
	return s.res.Form(ctx, s.id)
}

// quickNoteScreen is "/widget": the floating Quick note panel's page.
// The window is borderless, transparent, and non-activating
// (desktop.Widget), so this screen owns the whole surface: a ui.Card
// is the visual chrome, its header strip is the drag handle
// (data-fui-window-drag, wired by the runtime's desktop module), and
// the close button goes through the page script because only the page
// knows its own window id. Design-system components only (hard rules
// 7 and 8).
type quickNoteScreen struct {
	component.ContextOnly
}

func (s *quickNoteScreen) ScreenTitle() string       { return "Quick note" }
func (s *quickNoteScreen) ScreenDescription() string { return "The floating quick-note widget" }

func (s *quickNoteScreen) RenderCtx(ctx context.Context) render.HTML {
	header := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
		// The drag surface: mousedown here (or on a child) starts a
		// native window drag. html.Div is the 1:1 tag primitive; the
		// design-system components strip data-fui-* from ExtraAttrs,
		// and a drag handle is not a button.
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-fui-window-drag": ""}},
			render.Text("Quick note")),
		ui.Spacer(),
		ui.Button(ui.ButtonConfig{
			Label:      "Close",
			AriaLabel:  "Close the quick note widget",
			Variant:    ui.ButtonGhost,
			ExtraAttrs: html.Attrs{"data-notes-widget-close": ""},
		}),
	)
	form := ui.Form(ui.FormConfig{
		Action:      "/api/notes",
		Method:      "POST",
		SubmitLabel: "Add note",
		Ctx:         ctx,
		// The same entity route the editor form uses; on success the
		// form resets, ready for the next note.
		ExtraAttrs: interactive.Post("/api/notes").OnSuccess(interactive.ResetForm()).Attrs(),
	}, ui.FormField(ui.FormFieldConfig{
		Label: "Note",
		For:   "widget-note-title",
		Input: html.Input(html.InputConfig{
			Type:        "text",
			Name:        "title",
			ID:          "widget-note-title",
			Placeholder: "What is on your mind?",
		}),
	}))
	return ui.Card(ui.CardConfig{Header: header}, form)
}

// buildSite assembles the UI app and its screens. The island endpoint
// behind the list's sort/pagination is registered on the app router:
// the same table HTML the screen painted, fetched by RPC and swapped
// in place. IslandPolicy stays nil (signed-in callers only), which the
// desktop local identity (or a harness user) satisfies.
func buildSite(app *framework.App) (*appui.App, error) {
	site := appui.NewApp("desktop-notes")
	layout := appui.NewLayout("app").WithContainer()

	list := notesListResource(app)
	editorSrc := list.Crud

	site.Register("/", &notesListScreen{res: list}, layout)
	site.Register("/notes", &notesListScreen{res: list}, layout)
	site.Register("/notes/new", &noteEditorScreen{src: editorSrc}, layout)
	site.Register("/notes/{id}", &noteDetailScreen{res: list}, layout)
	site.Register("/notes/{id}/edit", &noteEditorScreen{src: editorSrc}, layout)
	// The widget window is borderless and transparent: its screen
	// renders in the chrome-less, transparent widget layout, never in
	// the app layout with its header and padded column.
	site.Register("/widget", &quickNoteScreen{}, appui.WidgetLayout())
	settings := settingsResource(app)
	site.Register("/settings", &settingsScreen{res: settings, crud: app.MustCrudHandler("settings")}, layout)
	site.Register("/settings/{id}", &settingsScreen{res: settings, crud: app.MustCrudHandler("settings")}, layout)

	app.Router().HandleFunc("GET", "/api/tables/notes", func(w http.ResponseWriter, r *http.Request) {
		list.TableHandler()(w, r)
	})
	return site, nil
}
