package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func renderSortable(p SortableListProps) string { return string(SortableList(p, nil)) }

func TestSortableListRendersTheRowsAndContract(t *testing.T) {
	h := renderSortable(SortableListProps{
		Label: "Priorities", RPCPath: "/api/reorder",
		Group: "board", Container: "todo", Version: "v3",
		ConflictRPC: "/api/conflict?col=todo",
		Items: []SortableItem{
			{Key: "a", Label: "First"},
			{Key: "b", Label: "Second"},
		},
	})
	for _, want := range []string{
		`data-hui-sortable=""`,
		`role="listbox"`,
		`aria-label="Priorities"`,
		`data-hui-sortable-rpc="/api/reorder"`,
		`data-hui-sortable-group="board"`,
		`data-hui-sortable-container="todo"`,
		`data-hui-sortable-version="v3"`,
		`data-hui-sortable-conflict="/api/conflict?col=todo"`,
		`data-hui-sortable-item=""`,
		`data-hui-sort-key="a"`,
		`draggable="true"`,
		`tabindex="0"`,
		`aria-roledescription="sortable item"`,
		`aria-label="Drag First"`,
		`>Second<`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("sortable list missing %q:\n%s", want, h)
		}
	}
	// Every row is an option of the listbox (axe aria-required-children).
	if got := strings.Count(h, `role="option"`); got != 2 {
		t.Errorf("want 2 rows with role=option, got %d:\n%s", got, h)
	}
	// The announcements travel as attributes, so the module never
	// says a sentence of its own.
	for _, want := range []string{
		`data-hui-sortable-s-grabbed="Grabbed {label}. Arrow keys to move, Space to drop."`,
		`data-hui-sortable-s-position="Position {position} in {list}."`,
		`data-hui-sortable-s-saved="Order saved."`,
		`data-hui-sortable-s-cancelled="Cancelled."`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("sortable list missing the announcement %q:\n%s", want, h)
		}
	}
}

func TestSortableListAnnouncementsComeFromStrings(t *testing.T) {
	h := renderSortable(SortableListProps{
		Label: "Priorités",
		Items: []SortableItem{{Key: "a", Label: "Premier"}},
		Strings: &Strings{
			SortableGrabbed:  "{label} saisi.",
			SortablePosition: "Position {position} dans {list}.",
		},
	})
	if !strings.Contains(h, `data-hui-sortable-s-grabbed="{label} saisi."`) {
		t.Errorf("the translated grabbed sentence never arrived:\n%s", h)
	}
	// A partial translation falls back: SortableDragLabel was left
	// empty, so the row's name is the English default.
	if !strings.Contains(h, `aria-label="Drag Premier"`) {
		t.Errorf("an unset SortableDragLabel must fall back to the English default:\n%s", h)
	}
}

func TestSortableListEmptyColumnStaysADropTarget(t *testing.T) {
	h := renderSortable(SortableListProps{Label: "Done", Group: "b", Container: "done", RPCPath: "/m"})
	if !strings.Contains(h, `data-hui-sortable=""`) {
		t.Errorf("an empty column must still render the sortable wrapper:\n%s", h)
	}
	// The wiring survives on the wrapper: an empty column is still a
	// drop target whose crossing must commit against its own RPC and
	// container, so the reconciliation and the commit both need them.
	for _, want := range []string{
		`data-hui-sortable-group="b"`,
		`data-hui-sortable-container="done"`,
		`data-hui-sortable-rpc="/m"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("an empty column must keep %s on the wrapper:\n%s", want, h)
		}
	}
	if strings.Contains(h, "<li") {
		t.Errorf("an empty column renders no rows:\n%s", h)
	}
}

// TestSortableListOmitsTheWiringItWasNotGiven: a plain list renders
// none of the kanban attributes, so a page that never opted into
// groups, containers or versioning carries none of their hooks.
func TestSortableListOmitsTheWiringItWasNotGiven(t *testing.T) {
	h := renderSortable(SortableListProps{Label: "L", RPCPath: "/m",
		Items: []SortableItem{{Key: "a", Label: "A"}}})
	for _, not := range []string{
		`data-hui-sortable-group`,
		`data-hui-sortable-container`,
		`data-hui-sortable-version`,
		`data-hui-sortable-conflict`,
	} {
		if strings.Contains(h, not) {
			t.Errorf("a list with no kanban wiring renders %s:\n%s", not, h)
		}
	}
}

func TestSortableListContentReplacesTheLabelBody(t *testing.T) {
	h := renderSortable(SortableListProps{
		Label: "L", Items: []SortableItem{
			{Key: "a", Label: "Hidden", Content: render.Raw("<b>Rich</b>")},
		},
	})
	if !strings.Contains(h, "<b>Rich</b>") {
		t.Errorf("Content should replace the label body:\n%s", h)
	}
	if strings.Contains(h, ">Hidden<") {
		t.Errorf("the label body should not double-render:\n%s", h)
	}
}

func TestSortableItemsRendersTheRowsWithoutTheWrapper(t *testing.T) {
	h := string(SortableItems(SortableListProps{
		Label: "To do", Items: []SortableItem{{Key: "k1", Label: "Design API"}},
	}, nil))
	if strings.Contains(h, "<ol") {
		t.Errorf("the reconciliation fragment carries no wrapper:\n%s", h)
	}
	if !strings.Contains(h, `data-hui-sortable-item=""`) {
		t.Errorf("the reconciliation fragment carries the rows:\n%s", h)
	}
	if h2 := string(SortableItems(SortableListProps{Label: "Empty"}, nil)); strings.Contains(h2, "<li") {
		t.Errorf("empty items reconcile to an empty fragment:\n%s", h2)
	}
}

func TestSortableListScrubsCarriedLabels(t *testing.T) {
	h := renderSortable(SortableListProps{
		Label: "L", Items: []SortableItem{{Key: "a", Label: "Ev\r\nil<script>"}},
	})
	if strings.ContainsAny(h, "\r\n") {
		t.Errorf("control bytes reached the DOM:\n%q", h)
	}
	if strings.Contains(h, "<script>") {
		t.Errorf("markup reached the DOM raw:\n%s", h)
	}
}

// TestSortableListKeysAreData: the key is what the database hands
// the page. A space, a quote, a hash or markup renders — escaped in
// the attribute, control bytes scrubbed — because a stored
// `foo "bar"` id must not take the page down at render; only the
// empty key refuses.
func TestSortableListKeysAreData(t *testing.T) {
	h := renderSortable(SortableListProps{Label: "L", Items: []SortableItem{
		{Key: "a b", Label: "A"},
		{Key: `x"y`, Label: "B"},
		{Key: "a#b", Label: "C"},
		{Key: "a<b>", Label: "D"},
		{Key: "it's", Label: "E"},
		{Key: "k\x00\r\n", Label: "F"},
	}})
	if strings.ContainsAny(h, "\r\n\x00") {
		t.Errorf("control bytes reached the key attribute:\n%q", h)
	}
	for _, want := range []string{
		`data-hui-sort-key="a b"`,
		`data-hui-sort-key="x&quot;y"`,
		`data-hui-sort-key="a#b"`,
		`data-hui-sort-key="a&lt;b&gt;"`,
		`data-hui-sort-key="it&#39;s"`,
		`data-hui-sort-key="k"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("a data-shaped key should render escaped, missing %q:\n%s", want, h)
		}
	}
}

func TestSortableListRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		call func()
	}{
		{"blank label", func() {
			SortableList(SortableListProps{Items: []SortableItem{{Key: "a", Label: "A"}}}, nil)
		}},
		{"empty key", func() {
			SortableList(SortableListProps{Label: "L", Items: []SortableItem{{Key: "", Label: "A"}}}, nil)
		}},
		{"blank item label", func() {
			SortableList(SortableListProps{Label: "L", Items: []SortableItem{{Key: "a", Label: " "}}}, nil)
		}},
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s should refuse at render", c.name)
				}
			}()
			c.call()
		}()
	}
}
