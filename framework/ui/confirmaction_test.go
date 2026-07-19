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
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q\nbody: %s", w, body)
		}
	}
	// Default (AutofocusConfirm=false): NEITHER button carries autofocus.
	// Cancel renders first in DOM order, so the Modal preset's
	// "focus first focusable" pass lands on it naturally — no need to
	// race with the platform's native autofocus pass.
	if strings.Contains(body, "autofocus") {
		t.Errorf("default ConfirmAction must not emit an autofocus attribute (would race with the Modal preset's focus pass and trigger Chrome's 'Autofocus processing was blocked' info message); body: %s", body)
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

// TestConfirmActionSuccessSignal wires the response-signal attribute on
// the Confirm button. The runtime reads data-fui-rpc-signal to decide
// where the 2xx body goes — without it, a ConfirmAction that mutates
// server state (e.g. delete) can never reconcile a list region.
func TestConfirmActionSuccessSignal(t *testing.T) {
	_, b := ConfirmAction(ConfirmActionConfig{
		Name:          "delete-row-1",
		TriggerLabel:  "Delete",
		Title:         "Delete row?",
		Body:          "Permanent.",
		RPCPath:       "/row/1/delete",
		SuccessSignal: "row-list",
	})
	body := string(b.Definition().Slots[0].Component.Render())
	dangerTag := substringFromTag(body, "<button", strings.Index(body, "ui-button--danger"))
	if !strings.Contains(dangerTag, `data-fui-rpc-signal="row-list"`) {
		t.Errorf("expected data-fui-rpc-signal=\"row-list\" on the Confirm button, got tag: %s", dangerTag)
	}
	// Sanity: the Cancel button must NOT carry the signal — only the
	// confirm action reconciles state.
	ghostTag := substringFromTag(body, "<button", strings.Index(body, "ui-button--ghost"))
	if strings.Contains(ghostTag, "data-fui-rpc-signal") {
		t.Errorf("did NOT expect data-fui-rpc-signal on Cancel, got tag: %s", ghostTag)
	}
}

// TestConfirmActionNoSignalByDefault confirms the opt-in nature of
// SuccessSignal: a fire-and-forget confirm renders no signal attribute.
func TestConfirmActionNoSignalByDefault(t *testing.T) {
	_, b := ConfirmAction(ConfirmActionConfig{
		Name:         "fire-and-forget",
		TriggerLabel: "Go",
		Title:        "Go?",
		Body:         "Now?",
		RPCPath:      "/go",
	})
	body := string(b.Definition().Slots[0].Component.Render())
	if strings.Contains(body, "data-fui-rpc-signal") {
		t.Errorf("expected NO data-fui-rpc-signal in body by default, got: %s", body)
	}
}

// TestConfirmActionSuccessSignalRejectedNames pins the
// selector-injection guard: the runtime interpolates the signal name
// verbatim into the CSS attribute selector
// `[data-fui-signal="<name>"]`, so any character outside [A-Za-z0-9_-]
// is either an invalid selector (silently no-ops) or a breakout
// (`x"],[data-fui-signal="secret`). Names that are safe MUST still pass.
func TestConfirmActionSuccessSignalRejectedNames(t *testing.T) {
	bad := []string{
		`x"],[data-fui-signal="secret`,
		`na"me`,
		`with space`,
		`hash#`,
		`dot.name`,
		`[bracket]`,
		`preserved"quote`,
		`non-ascii-é`,
		`back\slash`,
	}
	for _, name := range bad {
		t.Run("bad/"+name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for unsafe SuccessSignal name %q", name)
				}
			}()
			ConfirmAction(ConfirmActionConfig{
				Name:          "x",
				TriggerLabel:  "Delete",
				Title:         "T",
				Body:          "B",
				RPCPath:       "/x",
				SuccessSignal: name,
			})
		})
	}
	good := []string{"row-list", "opt_delete_list", "a", "A1", "n4", "list-1", "_underscore", "dash-"}
	for _, name := range good {
		t.Run("good/"+name, func(t *testing.T) {
			_, _ = ConfirmAction(ConfirmActionConfig{
				Name:          "x",
				TriggerLabel:  "Delete",
				Title:         "T",
				Body:          "B",
				RPCPath:       "/x",
				SuccessSignal: name,
			})
		})
	}
}
