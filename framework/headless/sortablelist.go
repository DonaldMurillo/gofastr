package headless

import (
	"net/http"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// sortableGripIcon is the six-dot grip: a sized svg, aria-hidden as decoration.
const sortableGripIcon render.HTML = `<svg width="14" height="14" viewBox="0 0 14 14" fill="currentColor" aria-hidden="true" xmlns="http://www.w3.org/2000/svg"><circle cx="5" cy="3" r="1.2"/><circle cx="5" cy="7" r="1.2"/><circle cx="5" cy="11" r="1.2"/><circle cx="9" cy="3" r="1.2"/><circle cx="9" cy="7" r="1.2"/><circle cx="9" cy="11" r="1.2"/></svg>`

// SortableList parts.
const (
	PartSortableItem Part = "sortable-item"
	PartSortableGrip Part = "sortable-grip"
)

// SortableItem is one row.
type SortableItem struct {
	Key     string      // required: the server applies the order by it; data, rendered escaped and scrubbed
	Label   string      // required: the row's text and its drag name
	Content render.HTML // replaces the label as the row's body; the grip and announcements keep Label
}

// SortableListProps configures one list.
type SortableListProps struct {
	Items       []SortableItem // in initial order; may be empty, and an empty list stays a drop target
	Label       string         // required: the list's accessible name
	RPCPath     string         // POSTed after every reorder: order, container, version and moved
	Group       string         // the board id linked lists share; rows move between lists of one Group
	Container   string         // this column's id, sent as the container field
	Version     string         // the concurrency token the rows rendered at; a 409 takes the conflict path
	ConflictRPC string         // GET on a 409: its rows replace the list's and its version the token

	ID         string
	ExtraAttrs html.Attrs

	Parts   Parts // nothing is fillable
	Strings *Strings
}

// sortableVersionHeader carries a renewed version, percent-encoded because header values are Latin-1.
const sortableVersionHeader = "X-Gofastr-Sortable-Version"

// RenewSortableVersion sets the version the server holds now on w: call it on the 2xx
// that accepts a move and on the ConflictRPC response. An empty version sets nothing.
func RenewSortableVersion(w http.ResponseWriter, version string) {
	if version == "" {
		return
	}
	w.Header().Set(sortableVersionHeader, url.PathEscape(version))
}

// SortableList renders the list.
func SortableList(p SortableListProps, s Classes) render.HTML {
	b := p.Parts.Box(s)
	return b.El("ol", PartRoot, sortableRootAttrs(p), sortableRows(p, b, false)...)
}

// SortableItems renders the rows without the <ol>, the fragment a ConflictRPC answers with.
func SortableItems(p SortableListProps, s Classes) render.HTML {
	return render.Join(sortableRows(p, p.Parts.Box(s), true)...)
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
		checkSameOrigin("SortableList", "RPCPath", p.RPCPath)
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
		checkSameOrigin("SortableList", "ConflictRPC", p.ConflictRPC)
		own["data-hui-sortable-conflict"] = p.ConflictRPC
	}
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

// sortableRows renders the rows; fragment is true when they have no wrapping <ol>.
func sortableRows(p SortableListProps, b Box, fragment bool) []render.HTML {
	w := p.Strings.Resolve()
	rows := make([]render.HTML, 0, len(p.Items))
	for _, it := range p.Items {
		if it.Key == "" {
			panic("headless: SortableList item Key is required — the server applies the new order by these keys; an empty key orders nothing")
		}
		checkLabel("SortableList item "+it.Key, "Label", it.Label)
		attrs := Attrs(map[string]string{
			"role":                 "option",
			"aria-roledescription": w.SortableItemRole,
			"aria-label":           w.fill(w.SortableDragLabel, "label", scrubControlBytes(it.Label)),
			"data-hui-sort-key":    scrubControlBytes(it.Key),
		})
		Mark(attrs, "data-hui-sortable-item")
		attrs["draggable"] = "true"
		attrs["tabindex"] = "0"

		gripOwn := Attrs(map[string]string{"aria-hidden": "true"})
		liOwn := attrs
		body := it.Content
		switch {
		case body != "":
			gripOwn = Internal(gripOwn)
		case fragment:
			gripOwn = Internal(gripOwn)
			body = b.El("span", PartLabel, Internal(nil), render.Text(scrubControlBytes(it.Label)))
		default:
			liOwn = Internal(attrs)
			body = b.El("span", PartLabel, nil, render.Text(scrubControlBytes(it.Label)))
		}
		rows = append(rows, b.El("li", PartSortableItem, liOwn,
			b.El("span", PartSortableGrip, gripOwn, sortableGripIcon),
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
