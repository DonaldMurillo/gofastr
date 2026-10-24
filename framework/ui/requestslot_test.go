package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

var (
	slotSearch = i18n.Define("test.requestslot.search", "Search")
	slotHome   = i18n.Define("test.requestslot.home", "Home")
)

// frenchCtx is a request context whose reader reads French, with each
// phrase in tr translated to its value.
func frenchCtx(tr map[i18n.Phrase]string) context.Context {
	cat := i18n.NewMapCatalog()
	for p, text := range tr {
		cat.Set("fr", p.Key(), i18n.Message{Text: text})
	}
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	return i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
}

// renderBody renders a mounted widget's body slot the way the widget
// server does: once per request, with that request's context.
func renderBody(t *testing.T, b *widget.Builder, ctx context.Context) string {
	t.Helper()
	def := b.Definition()
	for _, s := range def.Slots {
		if s.Name == "body" {
			out, err := component.SafeRenderCtx(ctx, s.Component)
			if err != nil {
				t.Fatal(err)
			}
			return string(out)
		}
	}
	t.Fatal("widget has no body slot")
	return ""
}

func TestConfirmDialogWordsPerRequest(t *testing.T) {
	_, b := ConfirmAction(ConfirmActionConfig{
		Name: "del", TriggerLabel: "Delete", Title: "Delete?", Body: "Gone for good.", RPCPath: "/del",
	})
	fr := renderBody(t, b, frenchCtx(map[i18n.Phrase]string{
		i18nui.DialogConfirm: "Confirmer", i18nui.DialogCancel: "Annuler",
	}))
	if !strings.Contains(fr, "Confirmer") || !strings.Contains(fr, "Annuler") {
		t.Fatalf("French request must get French buttons:\n%s", fr)
	}
	if en := renderBody(t, b, context.Background()); !strings.Contains(en, ">Confirm<") {
		t.Fatalf("the same mount must say English to an English reader:\n%s", en)
	}
}

func TestBellEmptyTextPerRequest(t *testing.T) {
	_, b := NotificationBell(NotificationBellConfig{Name: "bell", Label: "Notifications", Href: "/notifications"})
	fr := renderBody(t, b, frenchCtx(map[i18n.Phrase]string{i18nui.NotificationEmpty: "Aucune notification"}))
	if !strings.Contains(fr, "Aucune notification") {
		t.Fatalf("French request must get the French empty text:\n%s", fr)
	}
}

func TestShortcutSheetTitlePerRequest(t *testing.T) {
	_, b := ShortcutSheet(ShortcutSheetConfig{FallbackHref: "/keys", Items: []ShortcutItem{{Chord: "/", Label: "Search"}}})
	fr := renderBody(t, b, frenchCtx(map[i18n.Phrase]string{
		i18nui.ShortcutSheetTitle: "Raccourcis clavier", i18nui.CommandPaletteClose: "Fermer",
	}))
	if !strings.Contains(fr, "Raccourcis clavier") || !strings.Contains(fr, `aria-label="Fermer"`) {
		t.Fatalf("French request must get the French sheet words:\n%s", fr)
	}
}

func TestShortcutSheetFuncBuildsPerRequest(t *testing.T) {
	b := ShortcutSheetFunc(func(ctx context.Context) ShortcutSheetConfig {
		return ShortcutSheetConfig{FallbackHref: "/keys", Items: []ShortcutItem{{Chord: "/", Label: slotSearch.Text(ctx)}}}
	})
	fr := renderBody(t, b, frenchCtx(map[i18n.Phrase]string{slotSearch: "Rechercher"}))
	if !strings.Contains(fr, "Rechercher") {
		t.Fatalf("the rows must be built for the request:\n%s", fr)
	}
}

func TestCommandPalettePlaceholderPerRequest(t *testing.T) {
	_, b := CommandPalette(CommandPaletteConfig{RPCPath: "/palette", FallbackHref: "/search"})
	fr := renderBody(t, b, frenchCtx(map[i18n.Phrase]string{i18nui.CommandPalettePlaceholder: "Tapez une commande"}))
	if !strings.Contains(fr, "Tapez une commande") {
		t.Fatalf("French request must get the French placeholder:\n%s", fr)
	}
}

func TestCommandPaletteFuncBuildsPerRequest(t *testing.T) {
	b := CommandPaletteFunc(func(ctx context.Context) CommandPaletteConfig {
		return CommandPaletteConfig{FallbackHref: "/search", Commands: []PaletteCommand{{Label: slotHome.Text(ctx), Href: "/"}}}
	})
	fr := renderBody(t, b, frenchCtx(map[i18n.Phrase]string{slotHome: "Accueil"}))
	if !strings.Contains(fr, "Accueil") {
		t.Fatalf("the commands must be built for the request:\n%s", fr)
	}
}
