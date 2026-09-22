package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestRepeaterRendersItems(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:  "tags",
		Label: "Tags",
		Items: []render.HTML{
			render.Text("ITEM_A"),
			render.Text("ITEM_B"),
		},
	})
	for _, want := range []string{
		"fui-repeater",
		"Tags",
		"ITEM_A",
		"ITEM_B",
		"fui-repeater__item",
		"fui-repeater__add",
		"Add item",
		"Remove",
	} {
		mustContain(t, h, want)
	}
}

func TestRepeaterUsesTemplateWhenNoItems(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:     "links",
		MinItems: 2,
		Template: func(i int) render.HTML {
			return render.HTML(fmt.Sprintf("TPL_%d", i))
		},
	})
	mustContain(t, h, "TPL_0")
	mustContain(t, h, "TPL_1")
}

func TestRepeaterMinMaxAttrs(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:     "items",
		MinItems: 1,
		MaxItems: 5,
		Items:    []render.HTML{render.Text("x")},
	})
	// The floor and the ceiling speak through the controls themselves:
	// a row at the floor keeps a disabled remove, a full list a
	// disabled add. No min/max attr is carried for no one.
	mustContain(t, h, `data-hui-repeater-action="remove" disabled=""`)
}

func TestRepeaterRPCAttrs(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:    "items",
		RPCPath: "/api/items/repeater",
		Items:   []render.HTML{render.Text("x")},
	})
	mustContain(t, h, `data-fui-rpc="/api/items/repeater?op=add"`)
	mustContain(t, h, `data-fui-rpc="/api/items/repeater?index=0&amp;op=remove`)
}

func TestRepeaterHidesRemoveOnMinItems(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:     "items",
		MinItems: 1,
		Template: func(i int) render.HTML { return render.Text("t") },
	})
	// A row at the floor keeps its remove control, disabled: the row
	// is real and the control says it cannot go.
	if !strings.Contains(string(h), `data-hui-repeater-action="remove" disabled=""`) {
		t.Fatalf("expected disabled remove at the floor:\n%s", h)
	}
}

func TestRepeaterRPCPathWithExistingQuery(t *testing.T) {
	// If RPCPath already carries a query string, the action param must
	// be appended with `&`, not `?` (which produces an invalid URL).
	h := Repeater(RepeaterConfig{
		Name:    "items",
		RPCPath: "/api/items?tenant=42",
		Items:   []render.HTML{render.Text("x")},
	})
	s := string(h)
	if strings.Contains(s, `/api/items?tenant=42?action=add`) {
		t.Fatalf("double-? in RPC URL:\n%s", s)
	}
	// Strings are HTML-escaped so & becomes &amp;
	mustContain(t, h, `data-fui-rpc="/api/items?op=add&amp;tenant=42"`)
	mustContain(t, h, `data-fui-rpc="/api/items?index=0&amp;op=remove&amp;tenant=42`)
}

func TestRepeaterItemsAriaLive(t *testing.T) {
	// Add/remove must be announced to SR users: the status node is
	// the live region the module says the operation's outcome through.
	h := Repeater(RepeaterConfig{
		Name:  "items",
		Items: []render.HTML{render.Text("x")},
	})
	mustContain(t, h, `data-hui-repeater-status="" role="status"`)
}

func TestRepeaterCustomLabels(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:        "items",
		AddLabel:    "Add row",
		RemoveLabel: "Delete",
		Items:       []render.HTML{render.Text("x")},
	})
	mustContain(t, h, "Add row")
	mustContain(t, h, "Delete")
}

func TestRepeaterExtraAttrsOnRoot(t *testing.T) {
	h := Repeater(RepeaterConfig{Name: "items", ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Repeater root missing data-test:\n%s", root)
	}
}
