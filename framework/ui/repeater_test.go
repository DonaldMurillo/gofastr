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
		"ui-repeater",
		"Tags",
		"ITEM_A",
		"ITEM_B",
		"ui-repeater-item",
		"ui-repeater-add",
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
	mustContain(t, h, `data-min-items="1"`)
	mustContain(t, h, `data-max-items="5"`)
}

func TestRepeaterRPCAttrs(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:    "items",
		RPCPath: "/api/items/repeater",
		Items:   []render.HTML{render.Text("x")},
	})
	mustContain(t, h, `data-fui-rpc="/api/items/repeater?action=add"`)
	mustContain(t, h, `data-fui-rpc="/api/items/repeater?action=remove`)
}

func TestRepeaterHidesRemoveOnMinItems(t *testing.T) {
	h := Repeater(RepeaterConfig{
		Name:     "items",
		MinItems: 1,
		Template: func(i int) render.HTML { return render.Text("t") },
	})
	// The first item's remove button should be hidden (index 0 < MinItems 1)
	if !strings.Contains(string(h), `hidden`) {
		t.Fatalf("expected hidden attr on remove button for min-items:\n%s", h)
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
	mustContain(t, h, `data-fui-rpc="/api/items?tenant=42&amp;action=add"`)
	mustContain(t, h, `data-fui-rpc="/api/items?tenant=42&amp;action=remove`)
}

func TestRepeaterItemsAriaLive(t *testing.T) {
	// Add/remove must be announced to SR users.
	h := Repeater(RepeaterConfig{
		Name:  "items",
		Items: []render.HTML{render.Text("x")},
	})
	mustContain(t, h, `aria-live="polite"`)
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
