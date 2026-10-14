package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/service"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localrun"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// tablePath serves the run table alone; the list screen re-fetches it
// every pollEvery (data-cui-poll), so a run that starts or stops shows
// up without a reload.
const (
	tablePath = "/__runs/table"
	pollEvery = "5s"
)

// buildApp is the service.Builder: it runs in the daemon and the dev
// run, never for the other verbs.
func buildApp(rt service.Runtime) (*framework.App, error) {
	opts, err := rt.AppOptions()
	if err != nil {
		return nil, err
	}
	a := framework.NewApp(append(opts,
		framework.WithConfig(framework.AppConfig{Name: "gofastr-runs"}),
		framework.WithMCPTools(registerTools),
	)...)

	self := selfConfig(rt.Dev)
	site := app.NewApp("gofastr-runs")
	// The canonical theme carries the dark palette, so the OS colour
	// scheme applies.
	site.WithTheme(theme.Default())
	layout := app.NewLayout("app", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage}, l.Primary())
	})
	site.Register("/", &runsScreen{self: self}, layout)
	site.Register("/runs/{id}", &runScreen{}, layout)
	a.Mount(uihost.New(site))
	a.Router().Get(tablePath, http.HandlerFunc(serveTable))
	return a, nil
}

// selfConfig is the mcpServers entry for this tracker: its own binary
// with `service mcp`.
func selfConfig(dev bool) *mcpServer {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	args := []string{"service", "mcp"}
	if dev {
		args = append(args, "--dev")
	}
	return &mcpServer{Type: "stdio", Command: exe, Args: args}
}

// runsScreen is "/": every running gofastr app, and how to point an
// agent at this tracker.
type runsScreen struct {
	component.ContextOnly
	self *mcpServer
}

func (s *runsScreen) ScreenTitle() string { return "Runs" }
func (s *runsScreen) ScreenDescription() string {
	return "The gofastr apps running on this machine"
}
func (s *runsScreen) RenderCtx(ctx context.Context) render.HTML {
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    "Runs",
			Subtitle: "Every gofastr app running on this machine, from its local run record. The list refreshes every " + pollEvery + ".",
		}),
		html.Div(html.DivConfig{
			AriaLabel:  "Running apps",
			ExtraAttrs: html.Attrs{"data-cui-poll": pollEvery, "data-cui-poll-src": tablePath},
		}, runTable(ctx)),
	}
	if s.self != nil {
		parts = append(parts, ui.Card(ui.CardConfig{
			Heading:      "Connect an agent",
			HeadingLevel: 2,
			Description:  "Add this to the agent's MCP config. runs_list and run_get return the same list, each run with the entry that reaches its own MCP server.",
		}, ui.CodeBlock(ui.CodeBlockConfig{Code: configJSON("gofastr-runs", s.self), Language: "json", ShowCopy: true})))
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...)
}

// serveTable is the polled fragment: the run table alone.
func serveTable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(runTable(r.Context())))
}

func runTable(ctx context.Context) render.HTML {
	runs, err := listRuns()
	if err != nil {
		return ui.EmptyState(ui.EmptyStateConfig{
			Title:       "The run registry could not be read",
			Description: "Check that " + registryDir() + " is a directory this user can read.",
		})
	}
	rows := make([]ui.Row, 0, len(runs))
	for _, r := range runs {
		name := r.Name
		if r.Self {
			name += " (this app)"
		}
		mcp := "none"
		if r.MCP != nil {
			mcp = r.MCP.Type
		}
		rows = append(rows, ui.Row{ID: "run-" + r.ID, Cells: map[string]render.HTML{
			"name":    ui.Link(ui.LinkConfig{Href: "/runs/" + r.ID, Text: name}),
			"kind":    ui.Tag(ui.TagConfig{Label: r.Kind, Variant: kindVariant(r.Kind)}),
			"url":     ui.Link(ui.LinkConfig{Href: r.URL, Text: r.URL}),
			"mcp":     render.Text(mcp),
			"pid":     render.Text(strconv.Itoa(r.PID)),
			"started": startedAt(r.Started),
		}})
	}
	return ui.DataTable(ui.DataTableConfig{
		Ctx:     ctx,
		Caption: "Running apps",
		Columns: []ui.Column{
			{Key: "name", Header: "Name"},
			{Key: "kind", Header: "Kind"},
			{Key: "url", Header: "URL"},
			{Key: "mcp", Header: "MCP"},
			{Key: "pid", Header: "PID", Align: "end"},
			{Key: "started", Header: "Started"},
		},
		Rows:       rows,
		Responsive: ui.ResponsiveCards,
		Empty: ui.EmptyStateConfig{
			Title:       "No gofastr apps are running",
			Description: "Start one with `gofastr dev` or `go run`; it shows up here once its port is open.",
		},
	})
}

func kindVariant(kind string) ui.StatusVariant {
	switch kind {
	case "service":
		return ui.StatusSuccess
	case "dev", "service dev run":
		return ui.StatusInfo
	}
	return ui.StatusNeutral
}

func startedAt(t time.Time) render.HTML {
	return html.Time(html.TimeConfig{Datetime: t.Format(time.RFC3339)}, render.Text(t.Local().Format("Jan 2 15:04:05")))
}

// registryDir names the registry directory for an error message.
func registryDir() string {
	if d, err := localrun.Dir(); err == nil {
		return d
	}
	return "the run registry directory"
}

// configJSON is an mcpServers block naming one server.
func configJSON(name string, s *mcpServer) string {
	b, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{name: s}}, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

// runScreen is "/runs/{id}": one run, and the config that reaches it.
type runScreen struct {
	component.ContextOnly
	id string
}

func (s *runScreen) SetParams(p map[string]string) { s.id = p["id"] }
func (s *runScreen) ScreenTitle() string           { return "Run" }
func (s *runScreen) ScreenDescription() string     { return "One running gofastr app" }
func (s *runScreen) RenderCtx(ctx context.Context) render.HTML {
	back := ui.Link(ui.LinkConfig{Href: "/", Text: "All runs"})
	r, found, err := findRun(s.id)
	if err != nil || !found {
		return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			ui.PageHeader(ui.PageHeaderConfig{Title: "Run", Actions: back}),
			ui.EmptyState(ui.EmptyStateConfig{
				Title:       "This run is not running",
				Description: "It stopped, or the id is wrong. Its record goes away when its server does.",
				Action:      back,
			}),
		)
	}
	mcpText := "This app serves no MCP."
	var mcpBlock render.HTML
	if r.MCP != nil {
		mcpText = "Add this to the agent's MCP config to reach this app's own MCP server."
		mcpBlock = ui.CodeBlock(ui.CodeBlockConfig{Code: configJSON(configKey(r.Name), r.MCP), Language: "json", ShowCopy: true})
	}
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
		ui.PageHeader(ui.PageHeaderConfig{
			Title:   r.Name,
			Eyebrow: "Run",
			Badge:   ui.Tag(ui.TagConfig{Label: r.Kind, Variant: kindVariant(r.Kind)}),
			Actions: back,
		}),
		ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{
			{Label: "URL", Value: ui.Link(ui.LinkConfig{Href: r.URL, Text: r.URL})},
			{Label: "PID", Value: render.Text(strconv.Itoa(r.PID))},
			{Label: "Started", Value: startedAt(r.Started)},
			{Label: "Service", Value: render.Text(r.Service)},
			{Label: "Directory", Value: render.Text(r.Dir)},
			{Label: "Executable", Value: render.Text(r.Exe)},
			{Label: "Run id", Value: html.Code(html.TextConfig{}, render.Text(r.ID))},
		}}),
		ui.Card(ui.CardConfig{Heading: "Connect an agent", HeadingLevel: 2, Description: mcpText}, mcpBlock),
	)
}
