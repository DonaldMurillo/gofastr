package gallery

// Support code for the three stateful catalog demos (sortablelist,
// optimisticcreate, optimisticdelete), the sidebar + section-menu showcase
// configs, and the signal-store demo. These types and helpers ship in the
// gallery so the catalog's Demo closures are self-contained — a theme
// previewer or static export renders a realistic seed view of every demo
// without needing the docs site's per-visitor session machinery.
//
// examples/site keeps its own per-visitor demoState (cookies, bounded store,
// RPC handlers — see demo_state.go in that package) but delegates the actual
// rendering to these helpers, passing the visitor's current data in. So the
// SSR live view and the gallery's seed view share one render path.
//
// The naming follows the gallery convention (exported, because the package
// is a library): KanbanCard/KanbanColumn/OptimisticNote, InitialKanbanColumns,
// RenderKanbanBoard, etc.

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	patternsSortablelist "github.com/DonaldMurillo/gofastr/core-ui/patterns/sortablelist"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// =============================================================================
// Kanban board demo (catalog slug: sortablelist)
// =============================================================================

// KanbanCard is one draggable card on the kanban demo board.
type KanbanCard struct{ Key, Title string }

// KanbanColumn is one linked sortable column. A board is a []KanbanColumn.
type KanbanColumn struct {
	ID, Title string
	Cards     []KanbanCard
}

// InitialKanbanColumns is the seed board every visitor's session starts
// from — and the board the gallery's seed Demo closure renders.
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

// RenderKanbanBoard renders N linked sortable columns. Each column shares
// Group "kanban-demo" and has a unique Container id. Version + ConflictRPC
// wire the 409 conflict-recovery path. The caller owns any locking around
// the input data — this function is lock-free.
func RenderKanbanBoard(cols []KanbanColumn, version int) render.HTML {
	rendered := make([]render.HTML, 0, len(cols))
	for _, c := range cols {
		items := make([]patternsSortablelist.Item, len(c.Cards))
		for i, card := range c.Cards {
			items[i] = patternsSortablelist.Item{Key: card.Key, Label: card.Title}
		}
		rendered = append(rendered, html.Div(html.DivConfig{Class: "kanban-col"},
			html.Heading(html.HeadingConfig{Level: 3, Class: "kanban-col__title"},
				render.Text(c.Title)),
			patternsSortablelist.Render(patternsSortablelist.Config{
				Label:       c.Title,
				Group:       "kanban-demo",
				Container:   c.ID,
				RPCPath:     "/__site/sortable/move",
				Version:     fmt.Sprintf("v%d", version),
				ConflictRPC: "/__site/sortable/conflict?container=" + c.ID,
				Items:       items,
			}),
		))
	}
	return ui.Grid(ui.GridConfig{Min: "14rem", Gap: ui.GapMD}, rendered...)
}

// =============================================================================
// Optimistic UI demos (catalog slugs: optimisticcreate, optimisticdelete)
// =============================================================================

// OptimisticNote is one row in the optimistic-create / optimistic-delete
// demo lists. Each visitor's demoState carries two independent note lists
// — one per recipe — seeded from InitialOptimisticNotes.
type OptimisticNote struct {
	ID, Title string
}

// InitialOptimisticNotes is the snapshot every session's create + delete
// lists seed from. The delete list only shrinks (via the delete RPC); the
// create list grows past it as the user clicks Add.
var InitialOptimisticNotes = []OptimisticNote{
	{ID: "n1", Title: "Ship the optimistic-ui cookbook"},
	{ID: "n2", Title: "Document the mutation lifecycle"},
	{ID: "n3", Title: "Pair ConfirmAction with delete"},
}

// RenderOptimisticCreateList renders the create list as a <ul> of rows.
// Used at SSR and as the create-RPC response body: the runtime swaps the
// list region's innerHTML with this HTML on 2xx.
func RenderOptimisticCreateList(notes []OptimisticNote) render.HTML {
	items := make([]render.HTML, 0, len(notes))
	for _, n := range notes {
		items = append(items, html.ListItem(html.ListItemConfig{
			ExtraAttrs: html.Attrs{"data-opt-id": n.ID},
		}, render.Text(n.Title)))
	}
	if len(items) == 0 {
		// Empty state — the list region reconciles to zero items (#82
		// style), so the swap target is never a bare missing element.
		return html.Paragraph(html.TextConfig{Class: "ui-muted"},
			render.Text("No notes yet — click Add."))
	}
	return html.UnorderedList(html.ListConfig{Class: "demo-stack"}, items...)
}

// RenderOptimisticDeleteList renders the delete list as a <ul> with a
// Delete trigger per row. Each trigger is the ConfirmAction trigger for
// its row's modal (mounted by OptimisticDeleteModals).
func RenderOptimisticDeleteList(notes []OptimisticNote) render.HTML {
	items := make([]render.HTML, 0, len(notes))
	for _, n := range notes {
		trigger, _ := ui.ConfirmAction(ui.ConfirmActionConfig{
			Name:         "opt-delete-" + n.ID,
			TriggerLabel: "Delete",
			Title:        "Delete this note?",
			Body:         "It will be removed from the list.",
			RPCPath:      "/__site/optimistic/delete?id=" + n.ID,
			ConfirmLabel: "Delete it",
		})
		items = append(items, html.ListItem(html.ListItemConfig{
			ExtraAttrs: html.Attrs{"data-opt-id": n.ID},
		},
			html.Div(html.DivConfig{Class: "demo-row"},
				render.Text(n.Title),
				trigger,
			),
		))
	}
	if len(items) == 0 {
		return html.Paragraph(html.TextConfig{Class: "ui-muted"},
			render.Text("No notes — the list reconciled to zero. Reload to reset the demo."))
	}
	return html.UnorderedList(html.ListConfig{Class: "demo-stack"}, items...)
}

// OptimisticFailDeleteTrigger is the inline trigger for the
// "Delete (will fail)" affordance rendered below the list. It opens the
// dedicated opt-delete-fail-n1 modal (mounted by OptimisticDeleteModals)
// whose RPC always returns 422 — the runtime then leaves the opt-delete-list
// region untouched, exercising the optimistic-UI "failed delete leaves the
// row/list unchanged" invariant. Rendered as a free-standing button (no
// list row) so the store never carries a phantom "fail row".
func OptimisticFailDeleteTrigger() render.HTML {
	trigger, _ := ui.ConfirmAction(ui.ConfirmActionConfig{
		Name:         "opt-delete-fail-n1",
		TriggerLabel: "Delete n1 (will fail)",
		Title:        "Delete this note?",
		Body:         "The demo handler rejects this with 422 — the list must stay unchanged.",
		RPCPath:      "/__site/optimistic/delete/fail?id=n1",
		ConfirmLabel: "Delete it",
		// Bound to the same region as the working deletes: on 2xx the
		// runtime would swap the list; on the demo's 422 it skips the
		// swap (html-mode + non-string = no-op, see runtime.js).
		SuccessSignal: "opt-delete-list",
	})
	return trigger
}

// OptimisticDeleteModals returns one *widget.Builder per initial delete row
// (the ConfirmAction modal matching that row's Delete trigger), PLUS the
// opt-delete-fail-n1 modal for the "will fail" affordance. Hosts mount
// these once at startup, keyed off InitialOptimisticNotes — the set EVERY
// session seeds from and only ever shrinks below. So every rendered
// trigger, in any session, has its modal mounted — no orphan
// opt-delete-nN triggers.
func OptimisticDeleteModals() []*widget.Builder {
	out := make([]*widget.Builder, 0, len(InitialOptimisticNotes)+1)
	for _, n := range InitialOptimisticNotes {
		_, modal := ui.ConfirmAction(ui.ConfirmActionConfig{
			Name:         "opt-delete-" + n.ID,
			TriggerLabel: "Delete",
			Title:        "Delete this note?",
			Body:         "It will be removed from the list.",
			RPCPath:      "/__site/optimistic/delete?id=" + n.ID,
			ConfirmLabel: "Delete it",
			// SuccessSignal wires the 2xx response body (the fresh
			// shorter list HTML returned by the delete handler) into
			// the opt-delete-list signal region — the runtime swaps
			// the region's innerHTML, the row disappears, and the
			// modal closes via data-fui-rpc-close. On a non-2xx the
			// runtime leaves the region unchanged (html-mode skips
			// non-string values), which is the "failed delete leaves
			// the list unchanged" invariant.
			SuccessSignal: "opt-delete-list",
		})
		out = append(out, modal)
	}
	// "Will fail" affordance — same SuccessSignal, different RPC path
	// that always returns 422. Mounted once so its trigger is valid.
	_, failModal := ui.ConfirmAction(ui.ConfirmActionConfig{
		Name:          "opt-delete-fail-n1",
		TriggerLabel:  "Delete n1 (will fail)",
		Title:         "Delete this note?",
		Body:          "The demo handler rejects this with 422 — the list must stay unchanged.",
		RPCPath:       "/__site/optimistic/delete/fail?id=n1",
		ConfirmLabel:  "Delete it",
		SuccessSignal: "opt-delete-list",
	})
	out = append(out, failModal)
	return out
}

// RenderOptimisticCreateDemoFor renders the full optimistic-create demo
// (code block + Add button + the supplied list) for a given notes slice.
// The list region is bound to a signal in mode=html; on 2xx the runtime
// swaps its innerHTML with the response — the fresh authoritative list
// (with the new row's real server-assigned id). Hosts pass the visitor's
// current createNotes; the gallery's seed catalog closure passes
// InitialOptimisticNotes.
func RenderOptimisticCreateDemoFor(notes []OptimisticNote) render.HTML {
	list := RenderOptimisticCreateList(notes)
	addBtn := interactive.OnClick(
		ui.Button(ui.ButtonConfig{Label: "Add note"}),
		interactive.Post("/__site/optimistic/create").
			OnSuccess(interactive.SetSignal("opt-create-list")),
	)
	listRegion := interactive.BindHTML(html.Div(html.DivConfig{}, list), "opt-create-list")
	return html.Div(html.DivConfig{Class: "demo-stack"},
		ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `interactive.OnClick(
    ui.Button(ui.ButtonConfig{Label: "Add"}),
    interactive.Post("/__site/optimistic/create").
        OnSuccess(interactive.SetSignal("opt-create-list")),
)`}),
		addBtn,
		listRegion,
		html.Div(html.DivConfig{Class: "fact"},
			render.Text("The full list HTML is the response body. A true temp-row pattern (row visible before the RPC resolves, then replaced by the authoritative row on 2xx) needs an island with a small bit of registered JS — see the optimistic-ui doc, Recipe 3."),
		),
	)
}

// RenderOptimisticDeleteDemoFor renders the full optimistic-delete demo
// (code block + the supplied list + the will-fail trigger) for a given
// notes slice. Hosts pass the visitor's current deleteNotes; the gallery's
// seed catalog closure passes InitialOptimisticNotes.
func RenderOptimisticDeleteDemoFor(notes []OptimisticNote) render.HTML {
	list := RenderOptimisticDeleteList(notes)
	// ConfirmAction returns (trigger, modal). The trigger renders inline
	// per row; the host mounts the matching modals once at startup via
	// OptimisticDeleteModals().
	listRegion := interactive.BindHTML(html.Div(html.DivConfig{}, list), "opt-delete-list")
	return html.Div(html.DivConfig{Class: "demo-stack"},
		ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `trigger, modal := ui.ConfirmAction(ui.ConfirmActionConfig{
    Name:    "opt-delete-" + item.ID,
    RPCPath: "/__site/optimistic/delete?id=" + item.ID,
})
widget.Mount(app.Router(), modal.Build()) // once, at startup`}),
		listRegion,
		OptimisticFailDeleteTrigger(),
		html.Div(html.DivConfig{Class: "fact"},
			render.Text("Confirm → POST → on 2xx the response replaces the list region with the authoritative shorter list. On failure (4xx) the runtime skips the swap (html-mode + non-string value = no-op), so the row stays put — try “Delete n1 (will fail)” to see it. Pair with an Undo window for a true optimistic-remove pattern (Recipe 4)."),
		),
	)
}

// =============================================================================
// Sidebar + section-menu showcase configs (catalog slugs: sidebar, section-menu)
// =============================================================================

// SidebarShowcaseConfig is the config for the /components/sidebar demo. It
// is shared between the inline render (the hamburger trigger in the catalog
// closure) and the mounted ui-sidebar-drawer widget the host mounts once at
// startup — so the two agree on DrawerName + content, otherwise the
// hamburger opens a drawer that was never mounted.
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

// DemoSectionMenuConfig powers the /components/section-menu showcase — a
// small self-contained menu whose drawer the host mounts like any real
// menu's.
func DemoSectionMenuConfig() interactive.SectionMenuConfig {
	return interactive.SectionMenuConfig{
		AriaLabel:    "Demo sections",
		TriggerLabel: "Sections",
		DrawerName:   "demo-section-menu",
		Lead:         &interactive.SectionItem{Label: "Overview", Href: "#overview"},
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

// =============================================================================
// Signal-store demo (catalog slug: signal-store)
// =============================================================================

// DemoCompany is a page-scoped store slice powering the /components/signal-store
// demo: one producer renames it, every bound consumer updates client-side.
// Exported so hosts that want to inspect or reset the demo's slice can.
var DemoCompany = store.New("sitedemo").String("company", "Acme Corp")
