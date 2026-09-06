package main

import (
	"context"
	"net/http"
	"strconv"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/resource"
)

// The screens. All markup comes from framework/ui components (plus the
// resource engine's list/form/detail): the example ships zero CSS and
// zero hand-rolled structural markup (hard rules 7 and 8). The few
// html.Div/html.Paragraph/html.Heading uses are the 1:1 tag primitives
// carrying a data-focus-* hook, the same shape desktop-notes' drag
// handle uses.

// tasksResource is the /tasks list's config: the resource engine's
// list/detail/form screens for tasks.
func tasksResource(app *framework.App) resource.Config {
	return resource.Config{
		Entity:   "tasks",
		Title:    "Tasks",
		Singular: "Task",
		BasePath: "/tasks",
		APIPath:  "/api/tasks",
		Crud:     app.MustCrudHandler("tasks"),
		Fields: []resource.Field{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "estimate", Label: "Estimate", Type: "int"},
			{Key: "completed_pomodoros", Label: "Pomodoros", Type: "int"},
			{Key: "done", Label: "Done", Type: "bool"},
		},
	}.WithSearch("title").WithCreate().WithEdit().WithIsland("/api/tables/tasks")
}

// tasksFormResource is the editor's config: only the fields the form
// edits. The submit goes through the runtime's form intercept to the
// entity's REST route (POST /api/tasks, PUT /api/tasks/{id}).
func tasksFormResource(src resource.DataSource) resource.Config {
	return resource.Config{
		Entity:   "tasks",
		Title:    "Tasks",
		Singular: "Task",
		BasePath: "/tasks",
		APIPath:  "/api/tasks",
		Crud:     src,
		Fields: []resource.Field{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "note", Label: "Note", Type: "text"},
			{Key: "estimate", Label: "Estimate (pomodoros)", Type: "int"},
			{Key: "done", Label: "Done", Type: "bool"},
		},
	}
}

// taskDetailScreen is "/tasks/{id}" and taskEditorScreen is
// "/tasks/new" + "/tasks/{id}/edit", the notes example's landing
// shapes: Load resolves the title (owner-scoped, so a foreign id falls
// back, never leaks) and the editor's Cancel lands on the detail page.
type taskDetailScreen struct {
	component.ContextOnly
	res   resource.Config
	id    string
	title string
}

func (s *taskDetailScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *taskDetailScreen) Load(ctx context.Context) error {
	s.title = "Task"
	row, err := s.res.Crud.GetOne(ctx, s.id, nil)
	if err != nil || row == nil {
		return nil
	}
	if v, present := row["title"]; present {
		if t, ok := v.(string); ok && t != "" {
			s.title = t
		}
	}
	return nil
}

func (s *taskDetailScreen) ScreenTitle() string       { return s.title }
func (s *taskDetailScreen) ScreenDescription() string { return "A task" }

func (s *taskDetailScreen) RenderCtx(ctx context.Context) render.HTML {
	// A saved task lands here, so this is where a session starts: the
	// primary action the dashboard's row button repeats. The paragraph
	// names what Start does, since every one of those things happens
	// outside this page (a floating panel, the menu bar, a notification).
	start := ui.Card(ui.CardConfig{},
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Align: ui.AlignCenter},
			ui.Button(ui.ButtonConfig{
				Label:      "Start focus",
				AriaLabel:  "Start a focus session on " + s.title,
				Variant:    ui.ButtonPrimary,
				ExtraAttrs: map[string]string{"data-focus-start": s.id},
			}),
			html.Paragraph(html.TextConfig{}, render.Text("Opens the floating timer, counts down in the menu bar, and notifies you when the session ends.")),
		),
	)
	return render.Join(start, s.res.Detail(ctx, s.id))
}

type taskEditorScreen struct {
	component.ContextOnly
	src   resource.DataSource
	id    string
	title string
}

func (s *taskEditorScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *taskEditorScreen) Load(ctx context.Context) error {
	if s.id == "" {
		s.title = "New task"
		return nil
	}
	s.title = "Task"
	row, err := s.src.GetOne(ctx, s.id, nil)
	if err != nil || row == nil {
		return nil
	}
	if v, present := row["title"]; present {
		if t, ok := v.(string); ok && t != "" {
			s.title = t
		}
	}
	return nil
}

func (s *taskEditorScreen) ScreenTitle() string       { return s.title }
func (s *taskEditorScreen) ScreenDescription() string { return "Edit a task" }

func (s *taskEditorScreen) RenderCtx(ctx context.Context) render.HTML {
	return tasksFormResource(s.src).Form(ctx, s.id)
}

// dashboardScreen is "/": the page header, three stat cards, the "Now"
// card (phase, task, countdown, the four timer buttons), and the tasks
// table with its per-row Start link. The countdown and button
// visibility are server-rendered from the engine's state, then kept
// live by static/desktop-focus.js from focus_tick.
type dashboardScreen struct {
	component.ContextOnly
	app *framework.App
	eng *Engine
}

func (s *dashboardScreen) ScreenTitle() string       { return "Focus" }
func (s *dashboardScreen) ScreenDescription() string { return "Pomodoro focus timer" }

func (s *dashboardScreen) RenderCtx(ctx context.Context) render.HTML {
	stats := s.stats(ctx)
	state := s.eng.State(ctx)

	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:    "Focus",
		Subtitle: "One task, one timer, one break at a time",
		Actions: ui.LinkButton(ui.LinkButtonConfig{
			Label: "New task", Href: "/tasks/new", Variant: ui.ButtonPrimary,
		}),
	})
	cards := ui.Grid(ui.GridConfig{Min: "10rem"},
		ui.StatCard(ui.StatCardConfig{Label: "Focused today", Value: strconv.Itoa(stats.minutes) + "m"}),
		ui.StatCard(ui.StatCardConfig{Label: "Sessions today", Value: strconv.Itoa(stats.sessions)}),
		ui.StatCard(ui.StatCardConfig{Label: "Tasks done", Value: strconv.Itoa(stats.tasksDone)}),
	)
	now := nowCard(state)
	tasks := s.tasksTable(ctx, state.Phase)
	return render.Join(header, cards, now, tasks)
}

// nowCard is the "Now" card: what phase the timer is in, on which
// task, the countdown, and the controls. The data-focus-* hooks are
// the page script's; the hidden attributes are the server's best
// render of the same visibility rules the script applies on each
// focus_tick.
func nowCard(state State) render.HTML {
	phase := state.Phase
	countdown := "--:--"
	if phase != phaseIdle {
		countdown = trayClock(state.Remaining)
	}
	body := []render.HTML{
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-hint": ""}},
			render.Text("Start a session on a task below, or an untracked one here. The floating timer opens, the menu bar counts down, and a notification marks the end.")),
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-phase": ""}}, render.Text(phase)),
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-task": ""}}, render.Text(state.TaskTitle)),
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-countdown": ""}}, render.Text(countdown)),
	}
	controls := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
		focusButton("Start", "start", phase == phaseIdle, ui.ButtonPrimary),
		focusButton("Pause", "pause", phase == phaseWork || phase == phaseBreak, ui.ButtonSecondary),
		focusButton("Resume", "resume", phase == phasePaused, ui.ButtonSecondary),
		focusButton("Skip", "skip", phase != phaseIdle, ui.ButtonGhost),
	)
	return ui.Card(ui.CardConfig{Heading: "Now", HeadingLevel: 2}, append(body, controls)...)
}

// focusButton is one timer control: a plain button carrying the action
// hook, hidden unless the phase allows it.
func focusButton(label, action string, visible bool, variant ui.ButtonVariant) render.HTML {
	attrs := map[string]string{"data-focus-action": action}
	if !visible {
		attrs["hidden"] = ""
	}
	return ui.Button(ui.ButtonConfig{Label: label, Variant: variant, ExtraAttrs: attrs})
}

// tasksTable is the dashboard's task list: the resource engine's data
// source rendered as a DataTable with the per-row Start link the page
// script turns into focus.start.
func (s *dashboardScreen) tasksTable(ctx context.Context, phase string) render.HTML {
	rows, err := s.app.MustCrudHandler("tasks").ListAll(ctx, crud.ListOptions{
		Limit: 20,
		Sorts: []filter.ParsedSort{{Field: "created_at", Desc: true}},
	})
	if err != nil {
		return ui.Callout(ui.CalloutConfig{Title: "Couldn't load tasks", Variant: ui.StatusDanger},
			render.Text("See server logs."))
	}
	cols := []ui.Column{
		{Key: "title", Header: "Title"},
		{Key: "estimate", Header: "Estimate", Align: "end"},
		{Key: "completed_pomodoros", Header: "Pomodoros", Align: "end"},
		{Key: "done", Header: "Done"},
		{Key: "_a", Header: "", Align: "end"},
	}
	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		cells := map[string]render.HTML{
			"title":               render.Text(stringField(row, "title")),
			"estimate":            render.Text(strconv.Itoa(asInt(row["estimate"]))),
			"completed_pomodoros": render.Text(strconv.Itoa(asInt(row["completedPomodoros"]))),
			"done":                render.Text(boolField(row, "done")),
			"_a": render.Join(
				// Start is bridge-driven, not a navigation: the page
				// script turns it into focus.start. View is the plain
				// row link.
				ui.Button(ui.ButtonConfig{
					Label:      "Start",
					AriaLabel:  "Start a focus session on " + stringField(row, "title"),
					Variant:    ui.ButtonSecondary,
					ExtraAttrs: map[string]string{"data-focus-start": id},
				}),
				ui.Link(ui.LinkConfig{Href: "/tasks/" + id, Text: "View", Variant: ui.LinkAction}),
			),
		}
		uiRows = append(uiRows, ui.Row{ID: id, Cells: cells})
	}
	return ui.DataTable(ui.DataTableConfig{
		Columns:    cols,
		Rows:       uiRows,
		Responsive: ui.ResponsiveCards,
		Ctx:        ctx,
		Empty: ui.EmptyStateConfig{
			Title: "No tasks yet", Description: "Add one, then press Start on it: the floating timer opens and the menu bar counts down.", HeadingLevel: 2,
		},
	})
}

// dayStats is the three dashboard counters.
type dayStats struct {
	minutes   int
	sessions  int
	tasksDone int
}

// stats counts today's completed work sessions and done tasks through
// the owner-scoped handlers. Today's sessions are picked in Go, not in
// SQL: the comparison is on parsed timestamps, so it never depends on
// string-format affinity.
func (s *dashboardScreen) stats(ctx context.Context) dayStats {
	var out dayStats
	rows, err := s.app.MustCrudHandler("sessions").ListAll(ctx, crud.ListOptions{
		Limit: 200,
		Sorts: []filter.ParsedSort{{Field: "started_at", Desc: true}},
		Filters: []filter.ParsedFilter{
			{Field: "kind", Op: filter.OpEq, Value: "work"},
			(filter.ParsedFilter{Field: "completed", Op: filter.OpEq, Value: "true"}).Coerced(schema.Bool),
		},
	})
	if err != nil {
		return out
	}
	n := s.eng.now()
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location())
	for _, row := range rows {
		started := asTime(row["startedAt"])
		if started.Before(today) {
			break // newest first: everything after is older too
		}
		out.sessions++
		out.minutes += asInt(row["minutes"])
	}
	tasks, err := s.app.MustCrudHandler("tasks").ListAll(ctx, crud.ListOptions{
		Limit: 500,
		Filters: []filter.ParsedFilter{
			(filter.ParsedFilter{Field: "done", Op: filter.OpEq, Value: "true"}).Coerced(schema.Bool),
		},
	})
	if err == nil {
		out.tasksDone = len(tasks)
	}
	return out
}

// historyScreen is "/history": the sessions, newest first, as a
// read-only table (a session's truth is the engine; history is its
// log).
type historyScreen struct {
	component.ContextOnly
	app *framework.App
}

func (s *historyScreen) ScreenTitle() string       { return "History" }
func (s *historyScreen) ScreenDescription() string { return "Every focus session" }

func (s *historyScreen) RenderCtx(ctx context.Context) render.HTML {
	rows, err := s.app.MustCrudHandler("sessions").ListAll(ctx, crud.ListOptions{
		Limit: 100,
		Sorts: []filter.ParsedSort{{Field: "started_at", Desc: true}},
	})
	if err != nil {
		return ui.Callout(ui.CalloutConfig{Title: "Couldn't load history", Variant: ui.StatusDanger},
			render.Text("See server logs."))
	}
	cols := []ui.Column{
		{Key: "kind", Header: "Kind"},
		{Key: "task", Header: "Task"},
		{Key: "minutes", Header: "Minutes", Align: "end"},
		{Key: "started_at", Header: "Started"},
		{Key: "status", Header: "Status"},
	}
	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		kind, _ := row["kind"].(string)
		if kind == "" {
			kind = "work"
		}
		completed, _ := row["completed"].(bool)
		status := "completed"
		if !completed {
			status = "open"
		}
		started := asTime(row["startedAt"])
		uiRows = append(uiRows, ui.Row{ID: id, Cells: map[string]render.HTML{
			"kind":       render.Text(kind),
			"task":       render.Text(stringField(row, "taskId")),
			"minutes":    render.Text(strconv.Itoa(asInt(row["minutes"]))),
			"started_at": render.Text(started.Local().Format("15:04:05")),
			"status":     render.Text(status),
		}})
	}
	table := ui.DataTable(ui.DataTableConfig{
		Columns:    cols,
		Rows:       uiRows,
		Responsive: ui.ResponsiveCards,
		Ctx:        ctx,
		Empty: ui.EmptyStateConfig{
			Title: "No sessions yet", Description: "Start the timer and they land here.", HeadingLevel: 2,
		},
	})
	return render.Join(
		ui.PageHeader(ui.PageHeaderConfig{Title: "History", Subtitle: "Every focus session, newest first"}),
		table,
	)
}

// settingsResource is the settings form's config. BasePath is
// /settings; the engine's post-save target /settings/{id} is also a
// registered screen (the notes example's landing trick).
func settingsResource(app *framework.App) resource.Config {
	return resource.Config{
		Entity:   "settings",
		Title:    "Settings",
		Singular: "Settings",
		BasePath: "/settings",
		APIPath:  "/api/settings",
		Crud:     app.MustCrudHandler("settings"),
		Fields: []resource.Field{
			{Key: "work_minutes", Label: "Work minutes", Type: "int"},
			{Key: "break_minutes", Label: "Break minutes", Type: "int"},
			{Key: "notify", Label: "Notify when a session ends", Type: "bool"},
			{Key: "tray_countdown", Label: "Countdown in the menu bar", Type: "bool"},
		},
	}
}

// settingsScreen is "/settings" (and "/settings/{id}"): the owner's
// one settings row as the resource engine's form. The first visit
// creates the row with the declared defaults.
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
		"work_minutes":   defaultWorkMinutes,
		"break_minutes":  defaultBreakMinutes,
		"notify":         true,
		"tray_countdown": true,
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

// widgetScreen is "/widget": the floating timer's page. The window is
// borderless, transparent, and non-activating (desktop.Widget), so
// this screen owns the whole surface: a ui.Card is the visual chrome,
// its header strip is the drag handle (data-fui-window-drag), and
// Close/Open task go through the page script because only the page
// knows its own window id.
type widgetScreen struct {
	component.ContextOnly
	eng *Engine
}

func (s *widgetScreen) ScreenTitle() string       { return "Timer" }
func (s *widgetScreen) ScreenDescription() string { return "The floating focus timer" }

func (s *widgetScreen) RenderCtx(ctx context.Context) render.HTML {
	state := s.eng.State(ctx)
	title := state.TaskTitle
	if title == "" {
		title = "Focus"
	}
	countdown := "--:--"
	if state.Phase != phaseIdle {
		countdown = trayClock(state.Remaining)
	}

	header := ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
		// The drag surface: mousedown here (or on a child) starts a
		// native window drag. html.Div is the 1:1 tag primitive; the
		// design-system components strip data-fui-* from ExtraAttrs,
		// and a drag handle is not a button.
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-fui-window-drag": ""}},
			render.Text(title)),
		ui.Spacer(),
		ui.Button(ui.ButtonConfig{
			Label:      "Close",
			AriaLabel:  "Close the timer widget",
			Variant:    ui.ButtonGhost,
			ExtraAttrs: map[string]string{"data-focus-widget-close": ""},
		}),
	)

	controls := []render.HTML{
		focusButton("Start", "start", state.Phase == phaseIdle, ui.ButtonPrimary),
		focusButton("Pause", "pause", state.Phase == phaseWork || state.Phase == phaseBreak, ui.ButtonSecondary),
		focusButton("Resume", "resume", state.Phase == phasePaused, ui.ButtonSecondary),
		focusButton("Skip", "skip", state.Phase != phaseIdle, ui.ButtonGhost),
	}
	if state.TaskID != "" {
		controls = append(controls, ui.Button(ui.ButtonConfig{
			Label:      "Open task",
			Variant:    ui.ButtonSecondary,
			ExtraAttrs: map[string]string{"data-focus-open": state.TaskID},
		}))
	}

	return ui.Card(ui.CardConfig{Header: header},
		html.Heading(html.HeadingConfig{
			Level:      2,
			ExtraAttrs: html.Attrs{"data-focus-countdown": ""},
		}, render.Text(countdown)),
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-phase": ""}}, render.Text(state.Phase)),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, controls...),
	)
}

// buildSite assembles the UI app and its screens. The island endpoint
// behind the tasks list's sort/pagination is registered on the app
// router, the notes example's shape.
func buildSite(app *framework.App, eng *Engine) (*appui.App, error) {
	site := appui.NewApp("desktop-focus")
	layout := appui.NewLayout("app").WithContainer()

	tasks := tasksResource(app)
	site.Register("/", &dashboardScreen{app: app, eng: eng}, layout)
	app.Router().HandleFunc("GET", "/api/tables/tasks", func(w http.ResponseWriter, r *http.Request) {
		tasks.TableHandler()(w, r)
	})
	// The new-task form saves (and cancels) to the resource's BasePath,
	// so /tasks must be the dashboard with the timer, never the engine's
	// bare list: the first thing a user did landed on a page with no
	// Start button, and the whole timer looked gone.
	site.Register("/tasks", &dashboardScreen{app: app, eng: eng}, layout)
	site.Register("/tasks/new", &taskEditorScreen{src: tasks.Crud}, layout)
	site.Register("/tasks/{id}", &taskDetailScreen{res: tasks}, layout)
	site.Register("/tasks/{id}/edit", &taskEditorScreen{src: tasks.Crud}, layout)
	site.Register("/history", &historyScreen{app: app}, layout)
	// The widget window is borderless and transparent: its screen
	// renders in the chrome-less widget layout, never the app layout
	// with its header and padded column.
	site.Register("/widget", &widgetScreen{eng: eng}, appui.WidgetLayout())
	settings := settingsResource(app)
	site.Register("/settings", &settingsScreen{res: settings, crud: app.MustCrudHandler("settings")}, layout)
	site.Register("/settings/{id}", &settingsScreen{res: settings, crud: app.MustCrudHandler("settings")}, layout)
	return site, nil
}

func stringField(row map[string]any, key string) string {
	s, _ := row[key].(string)
	return s
}

func boolField(row map[string]any, key string) string {
	if b, ok := row[key].(bool); ok && b {
		return "yes"
	}
	return "no"
}
