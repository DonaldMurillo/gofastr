package ui

import (
	"testing"
)

func TestInlineEditRendersDisplayAndInput(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:  "title",
		Value: "Hello",
	})
	for _, want := range []string{
		"ui-inline-edit",
		"ui-inline-edit-display",
		`data-fui-inline-edit`,
		"Hello",
		"ui-inline-edit-input",
		"ui-inline-edit-original",
		`type="hidden"`,
	} {
		mustContain(t, h, want)
	}
}

func TestInlineEditEmptyText(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name: "title",
	})
	mustContain(t, h, "Click to edit")
	mustContain(t, h, `data-empty`)
}

func TestInlineEditCustomEmptyText(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:      "bio",
		EmptyText: "Add a bio",
	})
	mustContain(t, h, "Add a bio")
}

func TestInlineEditRPCPath(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:    "title",
		Value:   "x",
		RPCPath: "/api/edit-title",
	})
	mustContain(t, h, `data-fui-rpc="/api/edit-title"`)
	mustContain(t, h, `data-fui-rpc-method="POST"`)
}

func TestInlineEditRequired(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:     "title",
		Required: true,
	})
	mustContain(t, h, `required`)
}

func TestInlineEditCustomType(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:  "amount",
		Type:  "number",
		Value: "42",
	})
	mustContain(t, h, `type="number"`)
}

func TestInlineEditUsesProvidedID(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		ID:    "my-edit",
		Name:  "field",
		Value: "x",
	})
	mustContain(t, h, `data-fui-inline-edit="my-edit"`)
}

func TestInlineEditOriginalValuePreserved(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:  "title",
		Value: "Original Title",
	})
	// Should have hidden field preserving original value for revert
	mustContain(t, h, "ui-inline-edit-original")
	mustContain(t, h, "Original Title")
}

func TestInlineEditARIALabel(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:      "status",
		ARIALabel: "Edit status field",
		Value:     "active",
	})
	mustContain(t, h, `aria-label="Edit status field"`)
}

func TestInlineEditRoleAndTabindex(t *testing.T) {
	h := InlineEdit(InlineEditConfig{
		Name:  "title",
		Value: "x",
	})
	mustContain(t, h, `role="button"`)
	mustContain(t, h, `tabindex="0"`)
}
