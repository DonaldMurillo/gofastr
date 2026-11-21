package gallery

import (
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// KanbanCard is one draggable card on the kanban demo board.
type KanbanCard struct{ Key, Title string }

// KanbanColumn is one linked sortable column. A board is a []KanbanColumn.
type KanbanColumn struct {
	ID, Title string
	Cards     []KanbanCard
}

// InitialKanbanColumns is the seed board every visitor's session starts
// from, and the board the gallery's seed Demo closure renders.
func InitialKanbanColumns() []KanbanColumn {
	return []KanbanColumn{
		{ID: "todo", Title: "To do", Cards: []KanbanCard{
			{Key: "k1", Title: "Design API"},
			{Key: "k2", Title: "Write tests"},
		}},
		{ID: "doing", Title: "Doing", Cards: []KanbanCard{
			{Key: "k3", Title: "Build sortable kanban"},
		}},
		{ID: "done", Title: "Done", Cards: []KanbanCard{
			{Key: "k4", Title: "Read ARCHITECTURE.md"},
		}},
	}
}

// SortableConfig describes a Kanban column and its rows at the given board version.
func (c KanbanColumn) SortableConfig(version int) ui.SortableListConfig {
	items := make([]ui.SortableItem, len(c.Cards))
	for i, card := range c.Cards {
		items[i] = ui.SortableItem{Key: card.Key, Label: card.Title}
	}
	return ui.SortableListConfig{
		Label:       c.Title,
		Group:       "kanban-demo",
		Container:   c.ID,
		RPCPath:     "/__site/sortable/move",
		Version:     fmt.Sprintf("v%d", version),
		ConflictRPC: "/__site/sortable/conflict?container=" + c.ID,
		Items:       items,
	}
}

// RenderKanbanBoard renders the columns as linked sortable lists at the
// board's version. The caller owns any locking around cols.
func RenderKanbanBoard(cols []KanbanColumn, version int) render.HTML {
	rendered := make([]render.HTML, 0, len(cols))
	for _, c := range cols {
		rendered = append(rendered, ui.Stack(ui.StackConfig{Gap: ui.GapSM},
			html.Heading(html.HeadingConfig{Level: 3},
				render.Text(c.Title)),
			ui.SortableList(c.SortableConfig(version)),
		))
	}
	return ui.Grid(ui.GridConfig{Min: "14rem", Gap: ui.GapMD}, rendered...)
}

// OptimisticNote is one row in the optimistic-create and optimistic-delete
// demo lists.
type OptimisticNote struct {
	ID, Title string
}

// InitialOptimisticNotes is the snapshot every session's create and delete
// lists seed from.
var InitialOptimisticNotes = []OptimisticNote{
	{ID: "n1", Title: "Ship the optimistic-ui cookbook"},
	{ID: "n2", Title: "Document the mutation lifecycle"},
	{ID: "n3", Title: "Pair ConfirmAction with delete"},
}

// RenderOptimisticCreateList renders the create list, at SSR and as the
// create RPC's response body.
func RenderOptimisticCreateList(notes []OptimisticNote) render.HTML {
	items := make([]render.HTML, 0, len(notes))
	for _, n := range notes {
		items = append(items, listRow(html.Attrs{"data-opt-id": n.ID}, render.Text(n.Title)))
	}
	if len(items) == 0 {
		return html.Paragraph(html.TextConfig{}, ui.Muted(render.Text("No notes yet: click Add.")))
	}
	return rowList(items...)
}

// RenderOptimisticDeleteList renders the delete list with a Delete trigger
// per row for the modal OptimisticDeleteModals mounts.
func RenderOptimisticDeleteList(notes []OptimisticNote) render.HTML {
	items := make([]render.HTML, 0, len(notes))
	for _, n := range notes {
		trigger, _ := optimisticDeleteAction(n.ID)
		items = append(items, listRow(html.Attrs{"data-opt-id": n.ID},
			ui.Cluster(ui.ClusterConfig{Justify: ui.JustifyBetween},
				render.Text(n.Title),
				trigger,
			),
		))
	}
	if len(items) == 0 {
		return html.Paragraph(html.TextConfig{}, ui.Muted(render.Text("No notes: the list reconciled to zero. Reload to reset the demo.")))
	}
	return rowList(items...)
}

// OptimisticFailDeleteTrigger renders the "Delete (will fail)" trigger,
// whose modal's RPC always answers 422.
func OptimisticFailDeleteTrigger() render.HTML {
	trigger, _ := optimisticFailDeleteAction()
	return trigger
}

func optimisticDeleteAction(id string) (render.HTML, *widget.Builder) {
	return ui.ConfirmAction(ui.ConfirmActionConfig{
		Name:          "opt-delete-" + id,
		TriggerLabel:  "Delete",
		Title:         "Delete this note?",
		Body:          "It will be removed from the list.",
		RPCPath:       "/__site/optimistic/delete?id=" + id,
		ConfirmLabel:  "Delete it",
		SuccessSignal: "opt-delete-list",
	})
}

func optimisticFailDeleteAction() (render.HTML, *widget.Builder) {
	return ui.ConfirmAction(ui.ConfirmActionConfig{
		Name:          "opt-delete-fail-n1",
		TriggerLabel:  "Delete n1 (will fail)",
		Title:         "Delete this note?",
		Body:          "The demo handler rejects this with 422: the list must stay unchanged.",
		RPCPath:       "/__site/optimistic/delete/fail?id=n1",
		ConfirmLabel:  "Delete it",
		SuccessSignal: "opt-delete-list",
	})
}

// OptimisticDeleteModals returns the confirm modal of every initial delete
// row and of the will-fail trigger, for the host to mount once at startup.
func OptimisticDeleteModals() []*widget.Builder {
	out := make([]*widget.Builder, 0, len(InitialOptimisticNotes)+1)
	for _, n := range InitialOptimisticNotes {
		_, modal := optimisticDeleteAction(n.ID)
		out = append(out, modal)
	}
	_, failModal := optimisticFailDeleteAction()
	out = append(out, failModal)
	return out
}

// RenderOptimisticCreateDemoFor renders the optimistic-create demo around
// notes: its code, the Add button and the list region.
func RenderOptimisticCreateDemoFor(notes []OptimisticNote) render.HTML {
	list := RenderOptimisticCreateList(notes)
	addBtn := interactive.OnClick(
		ui.Button(ui.ButtonConfig{Label: "Add note"}),
		interactive.Post("/__site/optimistic/create").
			OnSuccess(interactive.SetSignal("opt-create-list")),
	)
	listRegion := interactive.BindHTML(html.Div(html.DivConfig{}, list), "opt-create-list")
	return stack(
		ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `interactive.OnClick(
    ui.Button(ui.ButtonConfig{Label: "Add"}),
    interactive.Post("/__site/optimistic/create").
        OnSuccess(interactive.SetSignal("opt-create-list")),
)`}),
		ui.Cluster(ui.ClusterConfig{}, addBtn),
		listRegion,
		note(
			render.Text("The full list HTML is the response body. A true temp-row pattern (row visible before the RPC resolves, then replaced by the authoritative row on 2xx) needs an island with a small bit of registered JS: see the optimistic-ui doc, Recipe 3."),
		),
	)
}

// RenderOptimisticDeleteDemoFor renders the optimistic-delete demo around
// notes: its code, the list region and the will-fail trigger.
func RenderOptimisticDeleteDemoFor(notes []OptimisticNote) render.HTML {
	list := RenderOptimisticDeleteList(notes)
	listRegion := interactive.BindHTML(html.Div(html.DivConfig{}, list), "opt-delete-list")
	return stack(
		ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `trigger, modal := ui.ConfirmAction(ui.ConfirmActionConfig{
    Name:          "opt-delete-" + item.ID,
    TriggerLabel:  "Delete",
    Title:         "Delete this note?",
    Body:          "It will be removed from the list.",
    RPCPath:       "/__site/optimistic/delete?id=" + item.ID,
    ConfirmLabel:  "Delete it",
    SuccessSignal: "opt-delete-list",
})
widget.MountBuilder(app.Router(), modal) // once, at startup`}),
		listRegion,
		ui.Cluster(ui.ClusterConfig{}, OptimisticFailDeleteTrigger()),
		note(
			render.Text("Confirm → POST → on 2xx the response replaces the list region with the authoritative shorter list. On failure (4xx) the runtime skips the swap (html-mode + non-string value = no-op), so the row stays put; try “Delete n1 (will fail)” to see it. Pair with an Undo window for a true optimistic-remove pattern (Recipe 4)."),
		),
	)
}

// SidebarShowcaseConfig is the sidebar demo's config, read by the catalog
// entry and by the drawer MountDemoWidgets mounts.
var SidebarShowcaseConfig = ui.SidebarConfig{
	Title:      "Docs",
	NavLabel:   "Sidebar component example",
	Variant:    ui.SidebarCollapsible,
	DrawerName: "ui-sidebar-drawer",
	Items: []ui.SidebarItem{
		{Label: "Modeling", Children: []ui.SidebarItem{
			{Label: "Entities", Href: "/docs/entities"},
			{Label: "Fields", Href: "/docs/fields"},
		}},
		{Label: "Serving", Children: []ui.SidebarItem{
			{Label: "Router", Href: "/docs/router"},
			{Label: "Middleware", Href: "/docs/middleware"},
		}},
	},
}

// DemoSectionMenuConfig is the section-menu demo's config.
func DemoSectionMenuConfig() interactive.SectionMenuConfig {
	return interactive.SectionMenuConfig{
		AriaLabel:     "Demo sections",
		TriggerLabel:  "Sections",
		DrawerName:    "demo-section-menu",
		InlineTrigger: true,
		Filter:        true,
		Lead:          &interactive.SectionItem{Label: "Overview", Href: "#overview"},
		Groups: []interactive.SectionGroup{
			{Eyebrow: "01", Label: "Modeling", Items: []interactive.SectionItem{
				{Label: "Entities", Href: "#entities", Active: true},
				{Label: "Filter DSL", Href: "#dsl"},
				{Label: "Relations", Href: "#relations"},
			}},
			{Eyebrow: "02", Label: "Serving", Collapsed: true, Items: []interactive.SectionItem{
				{Label: "Screens", Href: "#screens"},
				{Label: "Islands", Href: "#islands"},
			}},
		},
	}
}

// DemoCompany is the signal-store demo's page-scoped store slice.
var DemoCompany = store.New("sitedemo").String("company", "Acme Corp")

// DatePickerDemoEndpoint is the path a host serves the demo's month
// changes on, answering ui.DatePickerMonth(DatePickerDemo(month)).
const DatePickerDemoEndpoint = "/__site/datepicker/month"

// DatePickerDemo is the booking-calendar demo's config for month
// ("2006-01"; empty is November 2026).
func DatePickerDemo(month string) ui.DatePickerConfig {
	if month == "" {
		month = "2026-11"
	}
	first := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	shown, err := time.Parse("2006-01", month)
	if err != nil || shown.Before(first) {
		shown = first
	}
	days := map[string]ui.DatePickerDay{}
	for d := shown; d.Month() == shown.Month(); d = d.AddDate(0, 0, 1) {
		switch {
		case d.Day()%9 == 4:
			days[d.Format("2006-01-02")] = ui.DatePickerDay{Disabled: true, Note: "Full"}
		case d.Day()%7 == 5:
			days[d.Format("2006-01-02")] = ui.DatePickerDay{Note: "2 left"}
		}
	}
	return ui.DatePickerConfig{
		ID: "demo-stay", Label: "Your stay",
		Range:    &ui.DatePickerRange{StartName: "check_in", EndName: "check_out", Start: "2026-11-10", End: "2026-11-12", StartLabel: "Check-in", EndLabel: "Check-out"},
		Endpoint: DatePickerDemoEndpoint,
		Month:    month,
		Today:    "2026-11-02",
		Min:      "2026-11-01",
		Days:     days,
		Help:     "Full nights cannot be booked.",
	}
}
