package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestFormRepeaterRequiresName(t *testing.T) {
	defer func() { recover() }()
	FormRepeater(FormRepeaterConfig{})
	t.Fatal("expected panic without Name")
}

func TestFormRepeaterRendersEmpty(t *testing.T) {
	h := string(FormRepeater(FormRepeaterConfig{
		Name: "items",
	}))
	for _, want := range []string{
		`data-fui-comp="ui-form-repeater"`,
		"ui-form-repeater",
		">Add item<",
		`name="items_add"`,
		`value="1"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestFormRepeaterRendersItems(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "items[0].name", ID: "i0"})
	h := string(FormRepeater(FormRepeaterConfig{
		Name:  "items",
		Items: [][]render.HTML{{in}},
	}))
	for _, want := range []string{
		"ui-form-repeater__item",
		`data-index="0"`,
		">Remove<",
		`name="items_remove"`,
		`value="0"`,
		">Add item<",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestFormRepeaterMinItemsDisablesRemove(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(FormRepeater(FormRepeaterConfig{
		Name:     "items",
		Items:    [][]render.HTML{{in}},
		MinItems: 1,
	}))
	if !strings.Contains(h, `disabled`) {
		t.Errorf("Remove should be disabled when len(Items) <= MinItems, got: %s", h)
	}
}

// D-2: MinItems must not exceed MaxItems — creates impossible state
// where both add and remove are permanently disabled.
func TestFormRepeaterPanicOnMinExceedsMax(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("FormRepeater with MinItems > MaxItems should panic")
		}
	}()
	FormRepeater(FormRepeaterConfig{
		Name:     "items",
		Items:    [][]render.HTML{{render.Text("x")}},
		MinItems: 5,
		MaxItems: 3,
	})
}

func TestFormRepeaterMaxItemsDisablesAdd(t *testing.T) {
	in := html.Input(html.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := string(FormRepeater(FormRepeaterConfig{
		Name:     "items",
		Items:    [][]render.HTML{{in}},
		MaxItems: 1,
	}))
	if !strings.Contains(h, `disabled`) {
		t.Errorf("Add should be disabled when len(Items) >= MaxItems, got: %s", h)
	}
}

func TestFormRepeaterCustomLabels(t *testing.T) {
	h := string(FormRepeater(FormRepeaterConfig{
		Name:        "links",
		AddLabel:    "Add link",
		RemoveLabel: "Delete",
		Items:       [][]render.HTML{{render.Text("x")}},
	}))
	if !strings.Contains(h, ">Add link<") {
		t.Errorf("expected custom AddLabel, got: %s", h)
	}
	if !strings.Contains(h, ">Delete<") {
		t.Errorf("expected custom RemoveLabel, got: %s", h)
	}
}

func TestFormRepeaterMultipleItems(t *testing.T) {
	in1 := html.Input(html.InputConfig{Type: "text", Name: "n1", ID: "n1"})
	in2 := html.Input(html.InputConfig{Type: "text", Name: "n2", ID: "n2"})
	h := string(FormRepeater(FormRepeaterConfig{
		Name:  "items",
		Items: [][]render.HTML{{in1}, {in2}},
	}))
	if !strings.Contains(h, `data-index="0"`) {
		t.Errorf("missing index 0, got: %s", h)
	}
	if !strings.Contains(h, `data-index="1"`) {
		t.Errorf("missing index 1, got: %s", h)
	}
	// Two items, no MinItems, so Remove should not be disabled.
	if strings.Contains(h, `disabled`) {
		t.Errorf("Remove should NOT be disabled when > MinItems, got: %s", h)
	}
}

func TestFormRepeaterCustomClass(t *testing.T) {
	h := string(FormRepeater(FormRepeaterConfig{
		Name:  "items",
		Class: "extra",
	}))
	if !strings.Contains(h, "ui-form-repeater extra") {
		t.Errorf("expected custom class, got: %s", h)
	}
}

func TestFormRepeaterHasAriaLive(t *testing.T) {
	h := string(FormRepeater(FormRepeaterConfig{
		Name: "items",
	}))
	if !strings.Contains(h, `aria-live="polite"`) {
		t.Errorf("repeater container should have aria-live=polite, got:\n%s", h)
	}
}
