package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func sweepActionsPanics(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", what)
		}
	}()
	f()
}

func sweepActionsBefore(t *testing.T, h, first, second string) {
	t.Helper()
	i, j := strings.Index(h, first), strings.Index(h, second)
	if i < 0 || j < 0 {
		t.Fatalf("missing %q or %q:\n%s", first, second, h)
	}
	if i > j {
		t.Errorf("%q renders after %q:\n%s", first, second, h)
	}
}

func TestSweepActionsPaletteTriggerLinksToFallback(t *testing.T) {
	for _, kind := range []PaletteTrigger{PaletteTriggerHidden, PaletteTriggerField} {
		h := string(CommandPaletteTrigger(context.Background(), CommandPaletteConfig{RPCPath: "/s", FallbackHref: "/find", Trigger: kind}))
		if !strings.HasPrefix(strings.TrimSpace(h), "<a ") || !strings.Contains(h, `href="/find"`) || !strings.Contains(h, `data-cui-open="command-palette"`) {
			t.Errorf("trigger %q is not a link to the fallback that opens the palette:\n%s", kind, h)
		}
	}
}

func TestSweepActionsPaletteNeedsASource(t *testing.T) {
	sweepActionsPanics(t, "a palette with no RPCPath or Commands", func() { CommandPalette(CommandPaletteConfig{FallbackHref: "/s"}) })
}

func TestSweepActionsPaletteCommandsWinOverRPC(t *testing.T) {
	_, b := CommandPalette(CommandPaletteConfig{RPCPath: "/s", FallbackHref: "/s", Commands: []PaletteCommand{{Label: "Docs", Href: "/docs"}}})
	h := renderBody(t, b, context.Background())
	if strings.Contains(h, `data-cui-rpc=`) {
		t.Errorf("a palette with Commands still searches the endpoint:\n%s", h)
	}
	if !strings.Contains(h, `data-value="Docs"`) {
		t.Errorf("the static command is missing:\n%s", h)
	}
}

func TestSweepActionsPaletteComboboxIsInline(t *testing.T) {
	_, b := CommandPalette(CommandPaletteConfig{RPCPath: "/s", FallbackHref: "/s"})
	if h := renderBody(t, b, context.Background()); !strings.Contains(h, "data-hui-combobox-inline") {
		t.Errorf("the palette's combobox is not inline, so Escape stops at the listbox:\n%s", h)
	}
}

func TestSweepActionsConfirmActionCancelFirst(t *testing.T) {
	_, b := ConfirmAction(ConfirmActionConfig{Name: "d", TriggerLabel: "Delete", Title: "T", Body: "B", RPCPath: "/d"})
	sweepActionsBefore(t, renderBody(t, b, context.Background()), "fui-button--ghost", "fui-button--danger")
}

func TestSweepActionsConfirmTemplateCancelFirst(t *testing.T) {
	h := string(confirmDialogTemplate(context.Background()))
	sweepActionsBefore(t, h, `data-cui-confirm-part="cancel"`, `data-cui-confirm-part="accept"`)
	sweepActionsBefore(t, h, `data-cui-confirm-part="cancel"`, `data-cui-confirm-part="accept-danger"`)
	for _, want := range []string{`role="alertdialog"`, `aria-modal="true"`} {
		if !strings.Contains(h, want) {
			t.Errorf("template lacks %s:\n%s", want, h)
		}
	}
}

func TestSweepActionsConfirmTemplateWordsPerRequest(t *testing.T) {
	h := string(confirmDialogTemplate(frenchCtx(map[i18n.Phrase]string{
		i18nui.DialogConfirmTitle: "Êtes-vous sûr ?", i18nui.DialogCancel: "Annuler", i18nui.DialogConfirm: "Confirmer",
	})))
	for _, want := range []string{"Êtes-vous sûr ?", ">Annuler<", ">Confirmer<"} {
		if !strings.Contains(h, want) {
			t.Errorf("template lacks the reader's %q:\n%s", want, h)
		}
	}
}

func TestSweepActionsConfirmUnknownVariantIsDanger(t *testing.T) {
	trigger, _ := ConfirmAction(ConfirmActionConfig{Name: "d", TriggerLabel: "Delete", TriggerVariant: "loud", Title: "T", Body: "B", RPCPath: "/d"})
	if !strings.Contains(string(trigger), "fui-button--danger") {
		t.Errorf("an unknown variant did not fall back to danger:\n%s", trigger)
	}
}

func TestSweepActionsSignalNamesRefuseReservedKeys(t *testing.T) {
	for _, name := range []string{"__proto__", "constructor", "prototype"} {
		sweepActionsPanics(t, "ConfirmAction SuccessSignal "+name, func() {
			ConfirmAction(ConfirmActionConfig{Name: "d", TriggerLabel: "Delete", Title: "T", Body: "B", RPCPath: "/d", SuccessSignal: name})
		})
		sweepActionsPanics(t, "interactive.SetSignal "+name, func() { interactive.SetSignal(name) })
	}
}

func TestSweepActionsConfirmWithoutEndpointIsPlain(t *testing.T) {
	h := string((&confirmDialogSlot{titleID: "t", bodyID: "b", title: "T", body: "B", confirmLabel: "OK", cancelLabel: "Cancel"}).Render())
	if strings.Contains(h, "data-cui-rpc=") || strings.Contains(h, "data-cui-rpc-method") {
		t.Errorf("a confirm with no endpoint carries request wiring:\n%s", h)
	}
}

func TestSweepActionsCopyIconFallback(t *testing.T) {
	icon := string(CopyButton(CopyButtonConfig{Target: "c", IconOnly: true, Icon: "no-such-icon"}))
	if !strings.Contains(icon, "⧉") {
		t.Errorf("an IconOnly button with an unknown icon lost the copy glyph:\n%s", icon)
	}
	labelled := string(CopyButton(CopyButtonConfig{Target: "c", Icon: "no-such-icon"}))
	if strings.Contains(labelled, "fui-copy-btn__icon") {
		t.Errorf("a labelled button drew an icon for an unknown name:\n%s", labelled)
	}
}

func TestSweepActionsShortcutHintRefusesBadTarget(t *testing.T) {
	for _, sel := range []string{"#a\nb", "#a\x7f", "<b>"} {
		sweepActionsPanics(t, "BindTarget "+sel, func() { ShortcutHint(ShortcutHintConfig{Chord: "Mod+K", BindTarget: sel}) })
	}
	h := string(ShortcutHint(ShortcutHintConfig{Chord: "Mod+K", BindTarget: "#go"}))
	for _, want := range []string{`data-hui-shortcut-click="Mod+K"`, `data-hui-shortcut-target="#go"`} {
		if !strings.Contains(h, want) {
			t.Errorf("hint lacks %s:\n%s", want, h)
		}
	}
}

func TestSweepActionsAriaKeyShortcuts(t *testing.T) {
	for chord, want := range map[string]string{
		"Mod+S":       "Meta+S Control+S",
		"Shift+Mod+p": "Meta+Shift+P Control+Shift+P",
		"Alt+/":       "Alt+/",
		"Esc":         "Esc",
	} {
		if got := ariaKeyShortcuts(chord); got != want {
			t.Errorf("ariaKeyShortcuts(%q) = %q, want %q", chord, got, want)
		}
	}
}

func TestSweepActionsToolbarSkipsEmptyGroups(t *testing.T) {
	h := string(Toolbar(ToolbarConfig{Label: "Edit", Groups: []ToolbarGroup{{Label: "Empty"}, {Children: []render.HTML{render.Text("B")}}}}))
	if n := strings.Count(h, "fui-toolbar__group"); n != 1 || strings.Contains(h, "Empty") {
		t.Errorf("an empty group rendered (%d groups):\n%s", n, h)
	}
}

func TestSweepActionsSignalToggleEscapesClass(t *testing.T) {
	h := string(SignalToggle(SignalToggleConfig{SignalName: "on", Class: `x" onclick="alert(1)`}))
	if strings.Contains(h, `" onclick="`) {
		t.Errorf("a class broke out of its attribute:\n%s", h)
	}
}

func TestSweepActionsSearchInputLabelTargetsInput(t *testing.T) {
	for _, action := range []string{"", "/search"} {
		h := string(SearchInput(SearchInputConfig{Name: "q", ID: "find", Action: action}))
		if !strings.Contains(h, `<label class="fui-search" `) || !strings.Contains(h, `for="find"`) {
			t.Errorf("the frame is not a label for the input (Action %q):\n%s", action, h)
		}
	}
}

func TestSweepActionsSearchInputRefusesUnsafeAction(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "find", Action: "javascript:alert(1)"}))
	if strings.Contains(h, "javascript:") || !strings.Contains(h, `action="#"`) {
		t.Errorf("an unsafe Action reached the form:\n%s", h)
	}
}

func TestSweepActionsShortcutSheetTriggerOwnsItsWiring(t *testing.T) {
	h := string(ShortcutSheetTrigger(context.Background(), ShortcutSheetConfig{
		FallbackHref: "/keys", Items: []ShortcutItem{{Chord: "?", Label: "Help"}},
		ExtraAttrs: map[string]string{"href": "/evil", "aria-label": "evil", "data-hui-shortcut-click": "evil", "data-test": "hook"},
	}))
	if strings.Contains(h, "evil") || !strings.Contains(h, `href="/keys"`) || !strings.Contains(h, `data-test="hook"`) {
		t.Errorf("ExtraAttrs overrode the trigger's wiring:\n%s", h)
	}
	for _, href := range []string{"", "keys", "//evil.example", `/\evil`} {
		sweepActionsPanics(t, "FallbackHref "+href, func() {
			ShortcutSheetTrigger(context.Background(), ShortcutSheetConfig{FallbackHref: href, Items: []ShortcutItem{{Chord: "?", Label: "Help"}}})
		})
	}
	sweepActionsPanics(t, "no Items", func() { ShortcutSheetTrigger(context.Background(), ShortcutSheetConfig{FallbackHref: "/keys"}) })
}

func TestSweepActionsShortcutHintStaysExposed(t *testing.T) {
	h := string(ShortcutHint(ShortcutHintConfig{Chord: "Mod+K", ExtraAttrs: map[string]string{"aria-hidden": "true"}}))
	root := h[:strings.Index(h, ">")+1]
	if strings.Contains(root, `aria-hidden="true"`) || !strings.Contains(root, `aria-hidden="false"`) {
		t.Errorf("a caller hid the hint's screen-reader sentence:\n%s", root)
	}
}

func TestSweepActionsActionButtonsOwnTheirLifecycle(t *testing.T) {
	spoof := map[string]string{"type": "submit", "data-state": "spoofed", "aria-pressed": "mixed", "data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"OptimisticAction": OptimisticAction(OptimisticActionConfig{Endpoint: "/x", IdleLabel: "Follow", SuccessLabel: "Following", ExtraAttrs: spoof}),
		"ToggleAction":     ToggleAction(ToggleActionConfig{Endpoint: "/x", IdleLabel: "Follow", CommittedLabel: "Following", ExtraAttrs: spoof}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if strings.Contains(root, "submit") || strings.Contains(root, "spoofed") || strings.Contains(root, "mixed") || !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s ExtraAttrs reached the lifecycle attributes:\n%s", name, root)
		}
	}
}

func TestSweepActionsSignalToggleOwnsTheSwitch(t *testing.T) {
	h := string(SignalToggle(SignalToggleConfig{SignalName: "dark", Label: "Dark", ExtraAttrs: map[string]string{"role": "evil", "aria-checked": "evil", "aria-label": "evil"}}))
	root := h[:strings.Index(h, ">")+1]
	if strings.Contains(root, "evil") || !strings.Contains(root, `role="switch"`) {
		t.Errorf("ExtraAttrs overrode the switch contract:\n%s", root)
	}
}

func TestSweepActionsActionButtonsCheckVariantAndSize(t *testing.T) {
	sweepActionsPanics(t, "OptimisticAction variant", func() {
		OptimisticAction(OptimisticActionConfig{Endpoint: "/x", IdleLabel: "A", SuccessLabel: "B", Variant: "bogus"})
	})
	sweepActionsPanics(t, "OptimisticAction size", func() {
		OptimisticAction(OptimisticActionConfig{Endpoint: "/x", IdleLabel: "A", SuccessLabel: "B", Size: "huge"})
	})
	sweepActionsPanics(t, "ToggleAction size", func() {
		ToggleAction(ToggleActionConfig{Endpoint: "/x", IdleLabel: "A", CommittedLabel: "B", Size: "huge"})
	})
}

func TestSweepActionsFallbackHrefParity(t *testing.T) {
	refused := func(f func()) (r bool) {
		defer func() { r = recover() != nil }()
		f()
		return false
	}
	for _, href := range []string{"/ok", "/a/b?c=1", "", "#", "ok", "//evil.example", `/\evil`, `/x\evil`, "javascript:alert(1)", "https://x/y"} {
		palette := refused(func() {
			CommandPaletteTrigger(context.Background(), CommandPaletteConfig{RPCPath: "/s", FallbackHref: href})
		})
		sheet := refused(func() {
			ShortcutSheetTrigger(context.Background(), ShortcutSheetConfig{FallbackHref: href, Items: []ShortcutItem{{Chord: "?", Label: "Help"}}})
		})
		if palette != sheet {
			t.Errorf("FallbackHref %q: palette refuses %v, sheet refuses %v", href, palette, sheet)
		}
	}
}
