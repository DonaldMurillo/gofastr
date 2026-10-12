package main

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The screens. The entity's list, record and create pages come from
// the entityui builders, which draw framework/ui components from the
// entity's Display: the example ships zero CSS and zero hand-rolled
// structural markup (hard rules 7 and 8). List state (search, sort,
// page, view) rides the page's own query string; saves are form RPCs
// to the entity's REST routes.

// notesUI is the app's entity screen set, built once in buildSite
// after the entity registers. App.EntityUI checks every name the
// builders read against the declaration.
var notesUI *entityui.UI

// notesListScreen is "/" (and "/notes", the search form's target): the
// notes table with its search box, view tabs and New button. The
// builder draws it all from the entity's Display.
type notesListScreen struct {
	component.ContextOnly
}

func (s *notesListScreen) ScreenTitle() string       { return "Notes" }
func (s *notesListScreen) ScreenDescription() string { return "Local-first notes" }

func (s *notesListScreen) RenderCtx(ctx context.Context) render.HTML {
	// Base keeps record links and the New button on /notes even when
	// the list renders at "/".
	return notesUI.List("notes").Base("/notes").RenderCtx(ctx)
}

// noteDetailScreen is "/notes/{id}": the record page. Its Edit tab
// holds the editor form, so there is no separate edit route. Load
// resolves the open note's title so the page <title>, and through the
// page script the window title, follows the note; the lookup goes
// through the owner-scoped CRUD handler, so a foreign id renders "Not
// found", never another user's title.
type noteDetailScreen struct {
	component.ContextOnly
	ch    *crud.CrudHandler
	id    string
	title string
}

func (s *noteDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *noteDetailScreen) Load(ctx context.Context) error {
	s.title = "Note"
	row, err := s.ch.GetOne(ctx, s.id, nil)
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
	// Delete is the one record action the notes app turns on; the
	// header's copy link and back come with the page.
	return notesUI.Record("notes", s.id).Base("/notes").Delete().RenderCtx(ctx)
}

// noteCreateScreen is "/notes/create": the create form, posting to the
// entity's REST route and navigating back to the list on success.
type noteCreateScreen struct {
	component.ContextOnly
}

func (s *noteCreateScreen) ScreenTitle() string       { return "New note" }
func (s *noteCreateScreen) ScreenDescription() string { return "Create a note" }

func (s *noteCreateScreen) RenderCtx(ctx context.Context) render.HTML {
	return notesUI.Create("notes").Base("/notes").RenderCtx(ctx)
}

// The settings screen is not hand-built here: the battery's
// desktop.PreferencesScreen renders the form from the declared
// preferences (buildSite mounts it at /settings), and its POST
// /__gofastr/desktop/preferences route saves them.

// quickNoteScreen is "/widget": the floating Quick note panel's page.
// The window is borderless, transparent, and non-activating
// (desktop.Widget), so this screen owns the whole surface: a ui.Card
// is the visual chrome, its header strip is the drag handle
// (data-cui-window-drag, wired by the runtime's desktop module), and
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
		// design-system components strip data-cui-* from ExtraAttrs,
		// and a drag handle is not a button.
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-cui-window-drag": ""}},
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
		// The same entity route the record form uses; on success the
		// form resets, ready for the next note.
		ExtraAttrs: interactive.Post("/api/notes").OnSuccess(interactive.ResetForm()).Attrs(),
	}, ui.FormField(ui.FormFieldConfig{
		Label: "Note",
		For:   "widget-note-title",
		Input: func(fc headless.FieldControl) render.HTML {
			return ui.Control(ui.ControlConfig{
				Field:       fc,
				Type:        "text",
				Name:        "title",
				Placeholder: "What is on your mind?",
			})
		},
	}))
	return ui.Card(ui.CardConfig{Header: header}, form)
}

// buildSite assembles the UI app and its screens. The entity screen
// set is built here, after the entity registered: every list, record
// and create page reads through it, so no per-screen routes or
// handlers remain beyond the pages themselves.
func buildSite(app *framework.App, d *desktop.Battery) (*appui.App, error) {
	site := appui.NewApp("desktop-notes")
	layout := containerLayout("app")

	notesUI = app.EntityUI(entityui.Extensions{})
	notes := app.MustCrudHandler("notes")

	site.Register("/", &notesListScreen{}, layout)
	site.Register("/notes", &notesListScreen{}, layout)
	site.Register("/notes/create", &noteCreateScreen{}, layout)
	site.Register("/notes/{id}", &noteDetailScreen{ch: notes}, layout)
	// The widget window is borderless and transparent: its screen
	// renders in the chrome-less, transparent widget layout, never in
	// the app layout with its header and padded column.
	site.Register("/widget", &quickNoteScreen{}, desktopui.WidgetLayout())
	// The settings screen is the battery's: one form per declared
	// preference, saved through the battery's own route. There is no
	// /settings/{id}; the post-save landing is /settings itself.
	site.Register("/settings", desktop.PreferencesScreen(d, desktop.PreferencesScreenPath("/settings")), layout)
	return site, nil
}

// containerLayout is the plain page-column layout: the content in a
// centered ui.Container.
func containerLayout(name string) *appui.Layout {
	return appui.NewLayout(name, appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
}
