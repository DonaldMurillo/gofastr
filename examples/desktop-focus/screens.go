package main

import (
	"context"
	"strconv"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	desktopui "github.com/DonaldMurillo/gofastr/battery/desktop/ui"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entityui"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The screens. All markup comes from framework/ui components; the
// entity's record and create pages come from the entityui builders
// over them: the example ships zero CSS and zero hand-rolled
// structural markup (hard rules 7 and 8). The few html.Div /
// html.Paragraph / html.Heading uses are the 1:1 tag primitives
// carrying a data-focus-* hook, the same shape desktop-notes' drag
// handle uses.

// focusUI is the app's entity screen set, built once in buildSite
// after the entities register. App.EntityUI checks every name the
// builders read against the declarations.
var focusUI *entityui.UI

// taskCreateScreen is "/tasks/create": the create form, posting to the
// entity's REST route and landing on /tasks — the dashboard with the
// timer, where the new task's Start button is.
type taskCreateScreen struct {
	component.ContextOnly
}

func (s *taskCreateScreen) ScreenTitle() string       { return "New task" }
func (s *taskCreateScreen) ScreenDescription() string { return "Create a task" }

func (s *taskCreateScreen) RenderCtx(ctx context.Context) render.HTML {
	return focusUI.Create("tasks").Base("/tasks").RenderCtx(ctx)
}

// taskRecordScreen is "/tasks/{id}": the entityui record page. Its
// Edit tab holds the editor form, so there is no separate edit route.
// The "focus" tab is the app's own: the Start card a saved task can
// start a session from, and the task's facts in the desktop inspector
// pane. Load resolves the task's title so the page <title> follows the
// task; the lookup goes through the owner-scoped CRUD handler, so a
// foreign id renders "Not found", never another user's title.
type taskRecordScreen struct {
	component.ContextOnly
	ch    *crud.CrudHandler
	id    string
	title string
}

func (s *taskRecordScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *taskRecordScreen) Load(ctx context.Context) error {
	s.title = "Task"
	row, err := s.ch.GetOne(ctx, s.id, nil)
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

func (s *taskRecordScreen) ScreenTitle() string       { return s.title }
func (s *taskRecordScreen) ScreenDescription() string { return "A task" }
func (s *taskRecordScreen) RenderCtx(ctx context.Context) render.HTML {
	return focusUI.Record("tasks", s.id).Base("/tasks").Delete().RenderCtx(ctx)
}

// taskFocusTab is the record page's "Start" tab: what the old task
// page showed under its header. A saved task is what a session starts
// on, so this is where Start lives; the facts read better in the Mac
// inspector pane beside the content than in a full-width list under
// it, the Things and Reminders shape.
func taskFocusTab(tc entityui.TabContext) (component.Component, error) {
	return appui.NewStaticComponent(focusTab(tc)), nil
}

// focusTab draws the Start card, the note and the facts.
func focusTab(tc entityui.TabContext) render.HTML {
	row := tc.Record.Values
	id := tc.Record.ID
	title := stringField(row, "title")

	// A saved task is where a session starts: the primary action the
	// dashboard's row button repeats. The paragraph names what Start
	// does, since every one of those things happens outside this page
	// (a floating panel, the menu bar, a notification).
	start := ui.Card(ui.CardConfig{Heading: "Start a session", HeadingLevel: 2},
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD, Align: ui.AlignCenter},
			ui.Button(ui.ButtonConfig{
				Label:      "Start focus",
				AriaLabel:  "Start a focus session on " + title,
				Variant:    ui.ButtonPrimary,
				ExtraAttrs: map[string]string{"data-focus-start": id},
			}),
			html.Paragraph(html.TextConfig{}, render.Text("Opens the floating timer, counts down in the menu bar, and notifies you when the session ends.")),
		),
	)
	content := start
	if note := stringField(row, "note"); note != "" {
		content = render.Join(start, ui.Card(ui.CardConfig{Heading: "Note", HeadingLevel: 2},
			html.Paragraph(html.TextConfig{}, render.Text(note))))
	}

	facts := desktopui.Inspector(desktopui.InspectorConfig{
		Label: "Task facts",
		Title: "Details",
		Items: []ui.DetailItem{
			{Label: "Estimate", Value: render.Text(pomodoros(asInt(row["estimate"])))},
			{Label: "Completed", Value: render.Text(pomodoros(asInt(row["completedPomodoros"])))},
			{Label: "Done", Value: render.Text(boolField(row, "done"))},
		},
	})
	return desktopui.InspectorSplit(content, facts)
}

// pomodoros is n with its unit, singular for one.
func pomodoros(n int) string {
	if n == 1 {
		return "1 pomodoro"
	}
	return strconv.Itoa(n) + " pomodoros"
}

// dashboardScreen is "/": the page header, the floating timer toolbar,
// three stat cards, the "Now" card (phase, task, countdown), and the
// tasks table with its per-row Start link. The countdown and button
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
			Label: "New task", Href: "/tasks/create", Variant: ui.ButtonPrimary,
		}),
	})
	// The timer controls float in the glass capsule (the macOS
	// floating-toolbar group), sticky over the content while the task
	// list scrolls under them.
	toolbar := desktopui.FloatingToolbar(ui.ToolbarConfig{
		Label:  "Timer actions",
		Groups: []ui.ToolbarGroup{{Label: "Session", Children: timerControls(state.Phase)}},
	})
	cards := ui.Grid(ui.GridConfig{Min: "10rem"},
		ui.StatCard(ui.StatCardConfig{Label: "Focused today", Value: strconv.Itoa(stats.minutes) + "m"}),
		ui.StatCard(ui.StatCardConfig{Label: "Sessions today", Value: strconv.Itoa(stats.sessions)}),
		ui.StatCard(ui.StatCardConfig{Label: "Tasks done", Value: strconv.Itoa(stats.tasksDone)}),
	)
	now := nowCard(state)
	tasks := s.tasksTable(ctx, state.Phase)
	return render.Join(header, toolbar, cards, now, tasks)
}

// timerControls is the four timer buttons, visible per the phase. The
// data-focus-action hooks are the page script's; the hidden attributes
// are the server's best render of the same visibility rules the script
// applies on each focus_tick.
func timerControls(phase string) []render.HTML {
	return []render.HTML{
		focusButton("Start", "start", phase == phaseIdle, ui.ButtonPrimary),
		focusButton("Pause", "pause", phase == phaseWork || phase == phaseBreak, ui.ButtonSecondary),
		focusButton("Resume", "resume", phase == phasePaused, ui.ButtonSecondary),
		focusButton("Skip", "skip", phase != phaseIdle, ui.ButtonGhost),
	}
}

// nowCard is the "Now" card: what phase the timer is in, on which
// task, and the countdown. The data-focus-* hooks are the page
// script's, the same hooks the widget's countdown carries.
func nowCard(state State) render.HTML {
	phase := state.Phase
	countdown := "--:--"
	if phase != phaseIdle {
		countdown = trayClock(state.Remaining)
	}
	body := []render.HTML{
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-hint": ""}},
			render.Text("Start a session on a task below, or an untracked one from the toolbar. The floating timer opens, the menu bar counts down, and a notification marks the end.")),
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-phase": ""}}, render.Text(phase)),
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-task": ""}}, render.Text(state.TaskTitle)),
		html.Paragraph(html.TextConfig{ExtraAttrs: html.Attrs{"data-focus-countdown": ""}}, render.Text(countdown)),
	}
	return ui.Card(ui.CardConfig{Heading: "Now", HeadingLevel: 2}, body...)
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
	// A session stores its task's id; the log shows the task's title.
	// A task deleted since, or a session started from the toolbar with
	// no task, reads as untracked.
	titles := map[string]string{}
	if tasks, err := s.app.MustCrudHandler("tasks").ListAll(ctx, crud.ListOptions{Limit: 1000}); err == nil {
		for _, t := range tasks {
			titles[stringField(t, "id")] = stringField(t, "title")
		}
	}
	uiRows := make([]ui.Row, 0, len(rows))
	for _, row := range rows {
		id, _ := row["id"].(string)
		task := titles[stringField(row, "taskId")]
		if task == "" {
			task = "Untracked"
		}
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
			"task":       render.Text(task),
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

// The settings screen is not hand-built here: the battery's
// desktop.PreferencesScreen renders the form from the declared
// preferences (buildSite mounts it at /settings), and its POST
// /__gofastr/desktop/preferences route saves them.

// widgetScreen is "/widget": the floating timer's page. The window is
// borderless, transparent, and non-activating (desktop.Widget), so
// this screen owns the whole surface: a ui.Card is the visual chrome,
// its header strip is the drag handle (data-cui-window-drag), and
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
		// design-system components strip data-cui-* from ExtraAttrs,
		// and a drag handle is not a button.
		html.Div(html.DivConfig{ExtraAttrs: html.Attrs{"data-cui-window-drag": ""}},
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

// sidebarNav is the app's source list: the macOS sidebar the desktop
// layout places over the native sidebar material. The active row is
// marked from the live request path in the SSR bytes; after hydration
// the page script and the runtime's active-link module keep
// aria-current on the exact href match across client-side navigations
// (the script from first paint, the module once it idle-loads; see
// static/desktop-focus.js for why both). Sub-pages (/tasks/{id}) show
// no active row: exact matches only, and a server-side prefix rule
// would double-mark once the page clears what it owns.
func sidebarNav() component.Component {
	return appui.NewContextComponent(func(ctx context.Context) render.HTML {
		path := ""
		if r := appui.RequestFromContext(ctx); r != nil && r.URL != nil {
			path = r.URL.Path
		}
		return desktopui.SourceList(desktopui.SourceListConfig{
			Label: "Focus",
			Sections: []desktopui.SourceSection{{
				Title: "Focus",
				Items: []desktopui.SourceItem{
					{Label: "Dashboard", Href: "/"},
					{Label: "Tasks", Href: "/tasks"},
					{Label: "History", Href: "/history"},
				},
			}},
			CurrentPath: path,
		})
	})
}

// buildSite assembles the UI app and its screens: the desktop theme
// and the desktop layout with the source-list sidebar over the native
// sidebar material. The entity screen set is built here, after the
// entities registered.
func buildSite(app *framework.App, eng *Engine, d *desktop.Battery) (*appui.App, error) {
	site := appui.NewApp("desktop-focus")
	site.WithTheme(desktopui.Theme())
	layout := desktopui.Layout(sidebarNav())

	focusUI = app.EntityUI(entityui.Extensions{
		Entities: map[string]entityui.Extension{
			"tasks": {Tabs: []entityui.Tab{{Key: "focus", Label: "Start", Build: taskFocusTab}}},
		},
	})
	tasks := app.MustCrudHandler("tasks")

	site.Register("/", &dashboardScreen{app: app, eng: eng}, layout)
	// The create form saves (and cancels) to the tasks base, so /tasks
	// must be the dashboard with the timer, never a bare list: the
	// first thing a user did landed on a page with no Start button,
	// and the whole timer looked gone.
	site.Register("/tasks", &dashboardScreen{app: app, eng: eng}, layout)
	site.Register("/tasks/create", &taskCreateScreen{}, layout)
	site.Register("/tasks/{id}", &taskRecordScreen{ch: tasks}, layout)
	site.Register("/history", &historyScreen{app: app}, layout)
	// The widget window is borderless and transparent: its screen
	// renders in the chrome-less widget layout, never the desktop
	// layout with its sidebar and opaque content column.
	site.Register("/widget", &widgetScreen{eng: eng}, desktopui.WidgetLayout())
	// The settings screen is the battery's: one form per declared
	// preference, saved through the battery's own route. It lives in
	// the settings window only (the macOS shape: the app menu's
	// Settings… item, never a sidebar row), so it mounts on the
	// sidebar-less window layout over the whole-window material. There
	// is no /settings/{id}; the post-save landing is /settings itself.
	site.Register("/settings", desktop.PreferencesScreen(d, desktop.PreferencesScreenPath("/settings")), desktopui.WindowLayout())
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
