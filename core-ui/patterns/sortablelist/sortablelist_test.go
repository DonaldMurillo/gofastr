package sortablelist

import (
	"strings"
	"testing"
)

func TestRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SortableList without Label should panic")
		}
	}()
	Render(Config{Items: []Item{{Key: "a", Label: "A"}}})
}

func TestRequiresItems(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SortableList without Items should panic")
		}
	}()
	Render(Config{Label: "x"})
}

func TestItemRequiresKey(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Item without Key should panic")
		}
	}()
	Render(Config{Label: "x", Items: []Item{{Label: "A"}}})
}

func TestItemRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Item without Label should panic")
		}
	}()
	Render(Config{Label: "x", Items: []Item{{Key: "a"}}})
}

func TestEmitsListboxRole(t *testing.T) {
	h := string(Render(Config{
		Label: "Priorities",
		Items: []Item{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}},
	}))
	if !strings.Contains(h, `role="listbox"`) {
		t.Errorf("expected role=listbox:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Priorities"`) {
		t.Errorf("expected aria-label=Label:\n%s", h)
	}
}

func TestItemsAreDraggableWithKeys(t *testing.T) {
	h := string(Render(Config{
		Label: "x",
		Items: []Item{{Key: "task-1", Label: "Task 1"}, {Key: "task-2", Label: "Task 2"}},
	}))
	if c := strings.Count(h, `draggable="true"`); c != 2 {
		t.Errorf("expected 2 draggable items, got %d:\n%s", c, h)
	}
	if !strings.Contains(h, `data-fui-sort-key="task-1"`) {
		t.Errorf("expected sort-key=task-1:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-sortable-item="true"`) {
		t.Errorf("expected sortable-item marker:\n%s", h)
	}
}

func TestRPCPathEmittedOnList(t *testing.T) {
	h := string(Render(Config{
		Label: "x", RPCPath: "/api/reorder",
		Items: []Item{{Key: "a", Label: "A"}},
	}))
	if !strings.Contains(h, `data-fui-sortable-rpc="/api/reorder"`) {
		t.Errorf("expected sortable-rpc attr on list:\n%s", h)
	}
}

func TestItemCarriesPerRowAriaLabel(t *testing.T) {
	// axe nested-interactive: the row is interactive (focusable +
	// draggable), so the per-item drag label moved from a nested
	// <button> grip to the <li> itself.
	h := string(Render(Config{
		Label: "x",
		Items: []Item{{Key: "a", Label: "Buy milk"}},
	}))
	if !strings.Contains(h, `aria-label="Drag Buy milk"`) {
		t.Errorf("row should carry aria-label=Drag <Label>:\n%s", h)
	}
}
