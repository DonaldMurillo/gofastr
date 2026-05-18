package ui

import (
	"strings"
	"testing"
)

func TestConfirmActionTrigger(t *testing.T) {
	trigger, _ := ConfirmAction(ConfirmActionConfig{
		Name:         "delete-user-1",
		TriggerLabel: "Delete",
		Title:        "Delete user?",
		Body:         "Permanent.",
		RPCPath:      "/users/1/delete",
	})
	out := string(trigger)
	wants := []string{
		`<button`,
		`data-fui-open="delete-user-1"`,
		`ui-button ui-button--danger`,
		`>Delete</button>`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("trigger missing %q\ngot: %s", w, out)
		}
	}
}

func TestConfirmActionPanics(t *testing.T) {
	cases := []struct {
		name string
		cfg  ConfirmActionConfig
	}{
		{"no name", ConfirmActionConfig{TriggerLabel: "X", Title: "T", Body: "B", RPCPath: "/x"}},
		{"no trigger label", ConfirmActionConfig{Name: "n", Title: "T", Body: "B", RPCPath: "/x"}},
		{"no title", ConfirmActionConfig{Name: "n", TriggerLabel: "X", Body: "B", RPCPath: "/x"}},
		{"no body", ConfirmActionConfig{Name: "n", TriggerLabel: "X", Title: "T", RPCPath: "/x"}},
		{"no rpc", ConfirmActionConfig{Name: "n", TriggerLabel: "X", Title: "T", Body: "B"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for %s", c.name)
				}
			}()
			ConfirmAction(c.cfg)
		})
	}
}

func TestConfirmActionVariant(t *testing.T) {
	trigger, _ := ConfirmAction(ConfirmActionConfig{
		Name:           "save-draft",
		TriggerLabel:   "Save",
		TriggerVariant: "primary",
		Title:          "Save?",
		Body:           "Now?",
		RPCPath:        "/save",
	})
	if !strings.Contains(string(trigger), "ui-button--primary") {
		t.Errorf("expected ui-button--primary, got: %s", trigger)
	}
}

func TestConfirmActionSlotRendersAlertdialog(t *testing.T) {
	// Render the slot directly — this exercises the dialog HTML
	// shape independent of the widget chrome.
	_, b := ConfirmAction(ConfirmActionConfig{
		Name:         "delete-row-9",
		TriggerLabel: "Delete",
		Title:        "Delete row?",
		Body:         "This cannot be undone.",
		RPCPath:      "/row/9",
	})
	d := b.Definition()
	if d.Role != "alertdialog" {
		t.Errorf("expected Role=alertdialog, got %q", d.Role)
	}
	if d.LabelledBy != "delete-row-9-title" {
		t.Errorf("expected LabelledBy id, got %q", d.LabelledBy)
	}
	if d.DescribedBy != "delete-row-9-body" {
		t.Errorf("expected DescribedBy id, got %q", d.DescribedBy)
	}
	if !d.Hidden {
		t.Error("expected Hidden=true")
	}
	if !d.Backdrop {
		t.Error("expected Backdrop=true (Modal default)")
	}
	if !d.CloseOnEscape {
		t.Error("expected CloseOnEscape=true (Modal default)")
	}
	if len(d.Slots) != 1 || d.Slots[0].Name != "body" {
		t.Fatalf("expected one body slot, got %+v", d.Slots)
	}
	body := string(d.Slots[0].Component.Render())
	wants := []string{
		`id="delete-row-9-title"`,
		`id="delete-row-9-body"`,
		`Delete row?`,
		`This cannot be undone.`,
		`data-fui-rpc-close="`,
		`data-fui-rpc="/row/9"`,
		`data-fui-rpc-method="POST"`,
		`ui-button--danger`,
		`ui-button--ghost`,
		`autofocus="`, // on Cancel by default
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q\nbody: %s", w, body)
		}
	}
	// Cancel button must carry autofocus by default; Confirm must not.
	// Attrs are alphabetized; autofocus comes before class.
	if !strings.Contains(body, `autofocus=""`) {
		t.Errorf("default: expected autofocus attr present, body: %s", body)
	}
	// Locate the ghost button substring and confirm it contains autofocus.
	ghostIdx := strings.Index(body, "ui-button--ghost")
	dangerIdx := strings.Index(body, "ui-button--danger")
	if ghostIdx < 0 || dangerIdx < 0 {
		t.Fatalf("missing variant classes, body: %s", body)
	}
	// Walk back from each variant marker to the preceding "<button" and
	// check whether autofocus sits inside that button tag.
	ghostTag := substringFromTag(body, "<button", ghostIdx)
	dangerTag := substringFromTag(body, "<button", dangerIdx)
	if !strings.Contains(ghostTag, "autofocus") {
		t.Errorf("default: expected autofocus on Cancel (ghost), got tag: %s", ghostTag)
	}
	if strings.Contains(dangerTag, "autofocus") {
		t.Errorf("default: did NOT expect autofocus on Confirm (danger), got tag: %s", dangerTag)
	}
}

// substringFromTag returns the substring beginning at the LAST `tagOpen`
// before `idx` and ending at the first `>` after that point. Used by
// tests to extract a single tag from a glob of HTML when attribute order
// makes index-based slicing fragile.
func substringFromTag(html, tagOpen string, idx int) string {
	start := strings.LastIndex(html[:idx], tagOpen)
	if start < 0 {
		return ""
	}
	end := strings.Index(html[start:], ">")
	if end < 0 {
		return html[start:]
	}
	return html[start : start+end+1]
}

func TestConfirmActionAutofocusConfirm(t *testing.T) {
	_, b := ConfirmAction(ConfirmActionConfig{
		Name:             "apply-1",
		TriggerLabel:     "Apply",
		TriggerVariant:   "primary",
		Title:            "Apply?",
		Body:             "Looks good?",
		RPCPath:          "/apply",
		AutofocusConfirm: true,
	})
	body := string(b.Definition().Slots[0].Component.Render())
	dangerTag := substringFromTag(body, "<button", strings.Index(body, "ui-button--danger"))
	ghostTag := substringFromTag(body, "<button", strings.Index(body, "ui-button--ghost"))
	if !strings.Contains(dangerTag, "autofocus") {
		t.Errorf("expected autofocus on Confirm (danger) when AutofocusConfirm=true, got tag: %s", dangerTag)
	}
	if strings.Contains(ghostTag, "autofocus") {
		t.Errorf("did NOT expect autofocus on Cancel (ghost) when AutofocusConfirm=true, got tag: %s", ghostTag)
	}
}
