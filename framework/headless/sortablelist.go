package headless

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The sortable list: a reorderable <ol> whose rows move under HTML5
// drag-and-drop or the keyboard (Space grabs, the arrows move, Space
// drops, Escape cancels) and whose commit is a server round trip the
// server stays authoritative over — non-2xx reverts the DOM. The
// no-script contract is the list itself: a plain ordered list of
// rows, so a reader without script sees (and submits, if the caller
// wraps it in a form) exactly the rows the server rendered. The
// registered headless-sortablelist module binds the data-hui-sortable
// marker and owns the moves, the announcements (through Strings that
// travel as attributes, so a translated page announces in its own
// language) and the commit/rollback/conflict contract the pattern
// shipped: linked lists share a Group, a commit carries the
// destination order plus container and version fields, and a versioned
// 409 refetches fresh rows instead of a blanket rollback.

// sortableGripIcon is the six-dot drag grip: a real 14×14 svg with
// its own size, aria-hidden because the row's name rides its
// aria-label and the grip is decoration.
const sortableGripIcon render.HTML = `<svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor" aria-hidden="true" xmlns="http://www.w3.org/2000/svg"><circle cx="5" cy="3" r="1.2"/><circle cx="5" cy="7" r="1.2"/><circle cx="5" cy="11" r="1.2"/><circle cx="9" cy="3" r="1.2"/><circle cx="9" cy="7" r="1.2"/><circle cx="9" cy="11" r="1.2"/></svg>`

// SortableList parts.
const (
	PartSortableItem Part = "sortable-item"
	PartSortableGrip Part = "sortable-grip"
)

// SortableItem is one row.
type SortableItem struct {
	// Key is the stable identifier the server applies the new order
	// by. Required. It is data the database hands the page, not
	// configuration: a space, a quote, `#` or markup renders escaped
	// in its attribute and control bytes are scrubbed — only the
	// empty key refuses.
	Key string
	// Label is the row's visible text and the drag name. Required.
	Label string
	// Content, when set, replaces the label as the row's body. Use
	// for richer rows; the grip and the announcements still use
	// Label.
	Content render.HTML
}

// SortableListProps configures one list.
type SortableListProps struct {
	// Items are the rows in initial order. May be empty: an empty
	// column renders a valid sortable <ol> with no rows and stays a
	// drop target.
	Items []SortableItem
	// Label is the list's accessible name. Required.
	Label string
	// RPCPath, when set, is POSTed after every successful reorder:
	// order=<comma-separated keys>, plus container=<id> when
	// Container is set, version=<token> when Version is set, and
	// moved=<key> on a cross-container drop. The server confirms with
	// 2xx or rejects (the DOM reverts).
	RPCPath string
	// Group is the board id shared by linked lists (kanban): lists
	// with the same non-empty Group accept cross-container drag and
	// keyboard moves between them, including into an empty one.
	Group string
	// Container is the per-column id sent as the container field so
	// the server can route the write without inferring the column
	// from the key set. Distinct from Group because a board has one
	// Group and N containers.
	Container string
	// Version is an optional optimistic-concurrency token appended to
	// every commit. When set, a 409 fires the conflict path instead
	// of a blanket rollback.
	Version string
	// ConflictRPC, alongside Version, is GET-fetched on a 409: the
	// response body (fresh rows) replaces the list's contents,
	// server-rendered reconciliation. Without it a 409 falls back to
	// rollback.
	ConflictRPC string

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the root and the rows. No part is fillable:
	// the rows are the caller's data.
	Parts   Parts
	Strings *Strings
}

// SortableList renders the list.
func SortableList(p SortableListProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	return b.El("ol", PartRoot, sortableRootAttrs(p), sortableRows(p, b)...)
}

// SortableItems renders just the row elements without the <ol>
// wrapper, the fragment a conflict-recovery endpoint returns to
// replace a list's contents with the server's own rows. Empty items
// render an empty fragment: authoritative reconciliation may empty a
// column.
func SortableItems(p SortableListProps, s Classes) render.HTML {
	return render.Join(sortableRows(p, p.Parts.Box(s))...)
}

func sortableRootAttrs(p SortableListProps) html.Attrs {
	checkLabel("SortableList", "Label", p.Label)
	own := Merge(Safe(p.ExtraAttrs, "aria-label", "id"), Attrs(map[string]string{
		"id":         p.ID,
		"aria-label": p.Label,
		"role":       "listbox",
	}))
	Mark(own, "data-hui-sortable")
	if p.RPCPath != "" {
		own["data-hui-sortable-rpc"] = p.RPCPath
	}
	if p.Group != "" {
		own["data-hui-sortable-group"] = p.Group
	}
	if p.Container != "" {
		own["data-hui-sortable-container"] = p.Container
	}
	w := p.Strings.Resolve()
	if p.Version != "" {
		own["data-hui-sortable-version"] = p.Version
	}
	if p.ConflictRPC != "" {
		own["data-hui-sortable-conflict"] = p.ConflictRPC
	}
	// The module's announcements travel as attributes so a translated
	// page says them in its own language; the module substitutes the
	// {label}/{list}/{position} tokens when the move has happened.
	own["data-hui-sortable-s-grabbed"] = w.SortableGrabbed
	own["data-hui-sortable-s-position"] = w.SortablePosition
	own["data-hui-sortable-s-moved"] = w.SortableMoved
	own["data-hui-sortable-s-saved"] = w.SortableSaved
	own["data-hui-sortable-s-reverted"] = w.SortableReverted
	own["data-hui-sortable-s-cancelled"] = w.SortableCancelled
	own["data-hui-sortable-s-conflict-reverted"] = w.SortableConflictReverted
	own["data-hui-sortable-s-conflict-refreshed"] = w.SortableConflictRefreshed
	return own
}

func sortableRows(p SortableListProps, b Box) []render.HTML {
	w := p.Strings.Resolve()
	rows := make([]render.HTML, 0, len(p.Items))
	for _, it := range p.Items {
		// The key is data, not configuration: the database stamps it
		// and the page must render whatever it says. Only an empty
		// key refuses (the server applies the order by these keys);
		// everything else lands escaped in the attribute with its
		// control bytes scrubbed, and the module reads it back with
		// getAttribute — it never interpolates a key into a selector.
		if it.Key == "" {
			panic("headless: SortableList item Key is required — the server applies the new order by these keys; an empty key orders nothing")
		}
		checkLabel("SortableList item "+it.Key, "Label", it.Label)
		body := it.Content
		if body == "" {
			body = b.El("span", PartLabel, nil, render.Text(scrubControlBytes(it.Label)))
		}
		attrs := Attrs(map[string]string{
			// An option of the listbox root: a listbox with no option
			// children fails aria-required-children.
			"role":                 "option",
			"aria-roledescription": w.SortableItemRole,
			"aria-label":           fmt.Sprintf(w.SortableDragLabel, scrubControlBytes(it.Label)),
			"data-hui-sort-key":    scrubControlBytes(it.Key),
		})
		Mark(attrs, "data-hui-sortable-item")
		// draggable is enumerated: an empty value is "auto", and an
		// <li> under auto is not draggable, so pointer drag needs "true".
		attrs["draggable"] = "true"
		attrs["tabindex"] = "0"
		// The <li> itself is the focusable + draggable interactive
		// element (a focusable <li> must not contain a <button> —
		// axe nested-interactive); the grip is a decorative span and
		// the row's name rides its aria-label.
		rows = append(rows, b.El("li", PartSortableItem, attrs,
			b.El("span", PartSortableGrip, Attrs(map[string]string{"aria-hidden": "true"}),
				sortableGripIcon),
			body,
		))
	}
	return rows
}

func init() {
	Register(Spec{
		Name:    "SortableList",
		Anatomy: []Part{PartRoot, PartSortableItem, PartSortableGrip, PartLabel},
		Hooks: []string{
			"data-hui-sortable", "data-hui-sortable-item", "data-hui-sort-key",
			"data-hui-sortable-rpc", "data-hui-sortable-group",
			"data-hui-sortable-container", "data-hui-sortable-version",
			"data-hui-sortable-conflict",
			"data-hui-sortable-s-grabbed", "data-hui-sortable-s-position",
			"data-hui-sortable-s-moved", "data-hui-sortable-s-saved",
			"data-hui-sortable-s-reverted", "data-hui-sortable-s-cancelled",
			"data-hui-sortable-s-conflict-reverted", "data-hui-sortable-s-conflict-refreshed",
		},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return SortableList(SortableListProps{
				Label: "Priorities", RPCPath: "/api/reorder",
				Items: []SortableItem{
					{Key: "a", Label: "First"},
					{Key: "b", Label: "Second"},
				},
				Parts: parts,
			}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a single list",
				Why:  "the rows are a plain ordered list a reader without script sees whole; the module owns the moves, the announcements and the commit the server stays authoritative over",
				HTML: SortableList(SortableListProps{Label: "Priorities", RPCPath: "/api/reorder", Items: []SortableItem{
					{Key: "a", Label: "First"},
					{Key: "b", Label: "Second"},
				}}, s),
			}, {
				Name: "a linked column on a kanban board",
				Why:  "one column of a board: the shared Group lets rows cross between lists, the Container routes the write, and Version plus the conflict endpoint reconcile a 409 with the server's own rows instead of a rollback",
				HTML: SortableList(SortableListProps{
					Label: "To do", Group: "board-1", Container: "todo",
					RPCPath: "/api/move", Version: "v1", ConflictRPC: "/api/conflict?col=todo",
					Items: []SortableItem{{Key: "k1", Label: "Design API"}},
				}, s),
			}, {
				Name: "an empty column",
				Why:  "a list with no rows is still a valid drop target: the kanban's empty column accepts a dragged row, so the wrapper renders and the rows are simply absent",
				HTML: SortableList(SortableListProps{Label: "Done", Group: "board-1", Container: "done", RPCPath: "/api/move"}, s),
			}}
		},
	})
}

func init() {
	Register(Spec{
		Name:    "SortableItems",
		Anatomy: []Part{PartSortableItem, PartSortableGrip, PartLabel},
		Hooks:   []string{"data-hui-sortable-item", "data-hui-sort-key"},
		// No WithParts on purpose: the fragment is the rows alone, so
		// there is no root to route a bind to — a caller dressing the
		// rows reaches them through SortableListProps.Parts, whose
		// fixture is SortableList's.
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "the reconciliation fragment",
				Why:  "a versioned 409 replaces the list's rows with the server's own — this is that fragment, the rows without the wrapper (empty items render it empty, a column honestly emptied)",
				HTML: SortableItems(SortableListProps{
					Label: "To do", Items: []SortableItem{
						{Key: "k1", Label: "Design API"},
						{Key: "k2", Label: "Write tests"},
					},
				}, s),
			}}
		},
	})
}
