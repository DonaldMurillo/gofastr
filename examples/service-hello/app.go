package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/service"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// pulseEvery is how often the running service records that it is
// still up. The pulses are what the example has to show for running in
// the background: close the terminal, come back later, and the list
// has grown.
const pulseEvery = time.Minute

// buildApp is the service.Builder: it runs in the daemon and the dev
// run, never for the other verbs. rt carries the mode's state
// directory, where AppOptions opens app.db.
func buildApp(rt service.Runtime) (*framework.App, error) {
	opts, err := rt.AppOptions()
	if err != nil {
		return nil, err
	}
	app := framework.NewApp(append(opts,
		framework.WithConfig(framework.AppConfig{Name: "service-hello", APIPrefix: "api"}),
	)...)

	// The service's own log, written by the service. Nothing in it is
	// per-user data: the one person who can sign in is the OS user the
	// service runs as.
	app.Entity("pulses", framework.EntityConfig{
		Display: &framework.DisplayConfig{
			Singular:    "Pulse",
			Plural:      "Pulses",
			TitleFields: []string{"note"},
			Columns:     []string{"note", "created_at"},
		},
		Fields: []schema.Field{
			{Name: "note", Type: schema.String, Required: true, Max: new(200.0)},
		},
	})
	app.OnStart(func(ctx context.Context) error {
		go recordPulses(ctx, app.MustCrudHandler("pulses"), rt.Dev)
		return nil
	})

	pulsesUI = app.EntityUI(entityui.Extensions{})
	site := appui.NewApp("service-hello")
	layout := appui.NewLayout("app", appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
	site.Register("/", &pulsesScreen{}, layout)
	site.Register("/pulses", &pulsesScreen{}, layout)
	site.Register("/pulses/create", &pulseCreateScreen{}, layout)
	site.Register("/pulses/{id}", &pulseScreen{}, layout)
	app.Mount(uihost.New(site))
	return app, nil
}

// recordPulses writes one pulse at start and one every pulseEvery
// until the service stops.
func recordPulses(ctx context.Context, pulses *crud.CrudHandler, dev bool) {
	mode := "service"
	if dev {
		mode = "dev run"
	}
	write := func(note string) {
		if _, err := pulses.CreateOne(ctx, map[string]any{"note": note}); err != nil && ctx.Err() == nil {
			fmt.Fprintf(os.Stderr, "service-hello: record pulse: %v\n", err)
		}
	}
	write(fmt.Sprintf("%s started (pid %d)", mode, os.Getpid()))
	t := time.NewTicker(pulseEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			write(fmt.Sprintf("%s still running (pid %d)", mode, os.Getpid()))
		}
	}
}

// pulsesUI is the entity screen set, built once in buildApp.
var pulsesUI *entityui.UI

// pulsesScreen is "/" and "/pulses": the pulse list, newest first by
// the list's own sort state in the query string.
type pulsesScreen struct{ component.ContextOnly }

func (s *pulsesScreen) ScreenTitle() string       { return "Pulses" }
func (s *pulsesScreen) ScreenDescription() string { return "What the service did while you were away" }
func (s *pulsesScreen) RenderCtx(ctx context.Context) render.HTML {
	return pulsesUI.List("pulses").Base("/pulses").RenderCtx(ctx)
}

// pulseCreateScreen is "/pulses/create": a pulse written by hand.
type pulseCreateScreen struct{ component.ContextOnly }

func (s *pulseCreateScreen) ScreenTitle() string       { return "New pulse" }
func (s *pulseCreateScreen) ScreenDescription() string { return "Record a pulse by hand" }
func (s *pulseCreateScreen) RenderCtx(ctx context.Context) render.HTML {
	return pulsesUI.Create("pulses").Base("/pulses").RenderCtx(ctx)
}

// pulseScreen is "/pulses/{id}": one pulse, with Delete.
type pulseScreen struct {
	component.ContextOnly
	id string
}

func (s *pulseScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *pulseScreen) ScreenTitle() string           { return "Pulse" }
func (s *pulseScreen) ScreenDescription() string     { return "One pulse" }
func (s *pulseScreen) RenderCtx(ctx context.Context) render.HTML {
	return pulsesUI.Record("pulses", s.id).Base("/pulses").Delete().RenderCtx(ctx)
}
