// Package notesapp is the mobile prototype's dogfood app: desktop-notes'
// entity and screens with the desktop half removed (no menus, tray,
// windows or preferences battery). Screens come from the entityui
// builders, so the app ships zero CSS and zero hand-rolled markup; the
// design system's compact layouts carry the phone width.
package notesapp

import (
	"context"
	"fmt"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"

	"github.com/DonaldMurillo/gofastr-mobile/mobilehost"
)

// Build assembles the app on dataDir and attaches an in-process mobile
// host to it. The caller Starts the host.
func Build(dataDir, origin string) (*framework.App, *mobilehost.Host, error) {
	opts, err := mobilehost.Options(dataDir)
	if err != nil {
		return nil, nil, err
	}
	app := framework.NewApp(append(opts,
		framework.WithConfig(framework.AppConfig{Name: "Notes", APIPrefix: "api"}),
	)...)
	registerNotesEntity(app)
	app.Mount(uihost.New(buildSite(app)))
	h, err := mobilehost.New(app, mobilehost.Config{DataDir: dataDir, Origin: origin})
	if err != nil {
		return nil, nil, err
	}
	return app, h, nil
}

// registerNotesEntity is desktop-notes' entity: owner-scoped (hard rule
// 6) to the phone's local identity, which the mobile host installs.
func registerNotesEntity(app *framework.App) {
	app.Entity("notes", framework.EntityConfig{
		Scope:        &framework.ScopeConfig{OwnerField: "user_id"},
		SearchFields: []string{"title", "body"},
		Display: &framework.DisplayConfig{
			Singular:    "Note",
			Plural:      "Notes",
			TitleFields: []string{"title"},
			Columns:     []string{"title", "updated_at"},
			Fields: map[string]framework.FieldDisplay{
				"user_id": {Omit: true},
			},
		},
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "title", Type: schema.String, Required: true, Max: new(200.0)},
			{Name: "body", Type: schema.Text},
		},
	})
}

type notesListScreen struct {
	component.ContextOnly
	ui *entityui.UI
}

func (s *notesListScreen) ScreenTitle(context.Context) string       { return "Notes" }
func (s *notesListScreen) ScreenDescription(context.Context) string { return "Local-first notes" }
func (s *notesListScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.ui.List("notes").Base("/notes").RenderCtx(ctx)
}

type noteDetailScreen struct {
	component.ContextOnly
	ch    *crud.CrudHandler
	ui    *entityui.UI
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
			return fmt.Errorf("notesapp: title column is %T, want string", v)
		}
		if t != "" {
			s.title = t
		}
	}
	return nil
}

func (s *noteDetailScreen) ScreenTitle(context.Context) string       { return s.title }
func (s *noteDetailScreen) ScreenDescription(context.Context) string { return "A note" }
func (s *noteDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.ui.Record("notes", s.id).Base("/notes").Delete().RenderCtx(ctx)
}

type noteCreateScreen struct {
	component.ContextOnly
	ui *entityui.UI
}

func (s *noteCreateScreen) ScreenTitle(context.Context) string       { return "New note" }
func (s *noteCreateScreen) ScreenDescription(context.Context) string { return "Create a note" }
func (s *noteCreateScreen) RenderCtx(ctx context.Context) render.HTML {
	return s.ui.Create("notes").Base("/notes").RenderCtx(ctx)
}

func buildSite(app *framework.App) *appui.App {
	site := appui.NewApp("Notes")
	// The adaptive framework theme: a phone app follows the OS light/dark
	// setting (core-ui's bare default theme is light-only).
	site.WithTheme(theme.Default())
	layout := appui.NewLayout("app", appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
	notesUI := app.EntityUI(entityui.Extensions{})
	notes := app.MustCrudHandler("notes")
	site.Register("/", &notesListScreen{ui: notesUI}, layout)
	site.Register("/notes", &notesListScreen{ui: notesUI}, layout)
	site.Register("/notes/create", &noteCreateScreen{ui: notesUI}, layout)
	site.Register("/notes/{id}", &noteDetailScreen{ch: notes, ui: notesUI}, layout)
	return site
}
