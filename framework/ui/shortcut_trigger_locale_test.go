package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func TestShortcutTriggerRequestLocale(t *testing.T) {
	french := frenchCtx(map[i18n.Phrase]string{i18nui.ShortcutSheetTitle: "Raccourcis clavier"})
	for _, visible := range []bool{false, true} {
		cfg := ShortcutSheetConfig{Name: "keys", Shortcut: "Alt+H", FallbackHref: "/keys",
			Items: []ShortcutItem{{Chord: "/", Label: "Search"}}, TriggerVisible: visible, Ctx: french}
		mountedTrigger, body := ShortcutSheet(cfg)
		if got := ShortcutSheetTrigger(french, cfg); got != mountedTrigger {
			t.Fatal("constructor and request renderer disagree for the same context")
		}
		for _, request := range []struct {
			ctx   context.Context
			label string
		}{
			{context.Background(), "Keyboard shortcuts"},
			{french, "Raccourcis clavier"},
			{nil, "Keyboard shortcuts"},
		} {
			trigger := string(ShortcutSheetTrigger(request.ctx, cfg))
			for _, want := range []string{`aria-label="` + request.label + `"`, ">" + request.label + "<",
				`data-cui-open="keys"`, `data-hui-shortcut-click="Alt+H"`, `href="/keys"`} {
				if !strings.Contains(trigger, want) {
					t.Errorf("request trigger lacks %q: %s", want, trigger)
				}
			}
			if rendered := renderBody(t, body, request.ctx); !strings.Contains(rendered, ">"+request.label+"<") {
				t.Errorf("one mounted body lost its request title: %s", rendered)
			}
		}
	}
}

func TestShortcutTriggerKeepsCallerLabel(t *testing.T) {
	cfg := ShortcutSheetConfig{FallbackHref: "/keys", TriggerLabel: "Help & keys", TriggerVisible: true,
		Items: []ShortcutItem{{Chord: "/", Label: "Search"}}}
	ctx := frenchCtx(map[i18n.Phrase]string{i18nui.ShortcutSheetTitle: "Raccourcis clavier"})
	trigger := string(ShortcutSheetTrigger(ctx, cfg))
	if !strings.Contains(trigger, `aria-label="Help &amp; keys"`) || !strings.Contains(trigger, ">Help &amp; keys<") {
		t.Errorf("request trigger discarded the caller's label: %s", trigger)
	}
	_, body := ShortcutSheet(cfg)
	if rendered := renderBody(t, body, ctx); !strings.Contains(rendered, ">Help &amp; keys<") {
		t.Errorf("request body discarded the caller's title: %s", rendered)
	}
}
