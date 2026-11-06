package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func TestPaletteTriggerRequestLocale(t *testing.T) {
	french := frenchCtx(map[i18n.Phrase]string{
		i18nui.CommandPaletteOpen:        "Ouvrir les commandes",
		i18nui.CommandPalettePlaceholder: "Rechercher une commande",
		i18nui.CommandPaletteTitle:       "Commandes",
	})
	for _, variant := range []PaletteTrigger{PaletteTriggerHidden, PaletteTriggerField} {
		cfg := CommandPaletteConfig{Name: "commands", FallbackHref: "/search", RPCPath: "/commands", Trigger: variant, Ctx: french}
		mountedTrigger, body := CommandPalette(cfg)
		if got := CommandPaletteTrigger(french, cfg); got != mountedTrigger {
			t.Fatal("constructor and request renderer disagree for the same context")
		}
		for _, request := range []struct {
			ctx                       context.Context
			label, placeholder, title string
		}{
			{context.Background(), "Open command palette", "Type a command or search…", "Command palette"},
			{french, "Ouvrir les commandes", "Rechercher une commande", "Commandes"},
			{nil, "Open command palette", "Type a command or search…", "Command palette"},
		} {
			trigger := string(CommandPaletteTrigger(request.ctx, cfg))
			for _, want := range []string{`aria-label="` + request.label + `"`, `data-cui-open="commands"`, `data-hui-shortcut-click="Meta+K"`} {
				if !strings.Contains(trigger, want) {
					t.Errorf("request trigger lacks %q: %s", want, trigger)
				}
			}
			visible := request.label
			if variant == PaletteTriggerField {
				visible = request.placeholder
			}
			if !strings.Contains(trigger, ">"+visible+"<") {
				t.Errorf("request trigger lacks visible text %q: %s", visible, trigger)
			}
			if rendered := renderBody(t, body, request.ctx); !strings.Contains(rendered, request.title) || !strings.Contains(rendered, request.placeholder) {
				t.Errorf("one mounted body lost its request locale: %s", rendered)
			}
		}
	}
}

func TestPaletteTriggerKeepsOverrides(t *testing.T) {
	ctx := frenchCtx(map[i18n.Phrase]string{i18nui.CommandPalettePlaceholder: "Rechercher"})
	cfg := CommandPaletteConfig{FallbackHref: "/search", RPCPath: "/commands", Trigger: PaletteTriggerField,
		TriggerLabel: "Find & open", TriggerText: "Search my workspace", Placeholder: "Find a page", Shortcut: "Alt+P"}
	trigger := string(CommandPaletteTrigger(ctx, cfg))
	for _, want := range []string{`aria-label="Find &amp; open"`, ">Search my workspace<", `data-hui-shortcut-click="Alt+P"`} {
		if !strings.Contains(trigger, want) {
			t.Errorf("request trigger lacks caller override %q: %s", want, trigger)
		}
	}
	cfg.TriggerText = ""
	if trigger := string(CommandPaletteTrigger(ctx, cfg)); !strings.Contains(trigger, ">Find a page<") {
		t.Errorf("explicit placeholder no longer supplies field text: %s", trigger)
	}
}
