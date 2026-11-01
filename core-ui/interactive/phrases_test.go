package interactive

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// frenchInteractive carries a French translator for this package's words.
func frenchInteractive() context.Context {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", sectionsLabel.Key(), i18n.Message{Text: "Rubriques"})
	cat.Set("fr", filterLabel.Key(), i18n.Message{Text: "Filtrer"})
	cat.Set("fr", noMatches.Key(), i18n.Message{Text: "Aucun résultat"})
	cat.Set("fr", closeLabel.Key(), i18n.Message{Text: "Fermer"})
	cat.Set("fr", menuLabel.Key(), i18n.Message{Text: "Sommaire"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	return i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
}

func TestSectionMenuSpeaksCtx(t *testing.T) {
	cfg := SectionMenuConfig{Filter: true, Groups: []SectionGroup{{Label: "Docs", Items: []SectionItem{{Label: "Intro", Href: "/intro"}}}}}
	en := string(SectionMenu(cfg))
	cfg.Ctx = frenchInteractive()
	fr := string(SectionMenu(cfg))
	for _, w := range []string{"Rubriques", "Filtrer", "Aucun résultat"} {
		if !strings.Contains(fr, w) {
			t.Errorf("French ctx: %q missing from\n%s", w, fr)
		}
	}
	for _, w := range []string{"Sections", "Filter", "No matches"} {
		if !strings.Contains(en, w) {
			t.Errorf("nil ctx: %q missing from\n%s", w, en)
		}
	}
}

// The default trigger label reaches the pill, its sr-only prefix and
// the sheet title: all three follow the reader's locale.
func TestSectionMenuDefaultLabelSpeaksCtx(t *testing.T) {
	cfg := SectionMenuConfig{DrawerName: "docs-nav", Groups: []SectionGroup{{Label: "Docs", Items: []SectionItem{{Label: "Intro", Href: "/intro"}}}}}
	if en := string(SectionMenu(cfg)) + string(sectionMenuDrawerSlot{cfg: cfg}.Render()); !strings.Contains(en, ">Menu<") {
		t.Errorf("nil ctx: English Menu label missing from\n%s", en)
	}
	cfg.Ctx = frenchInteractive()
	fr := string(SectionMenu(cfg)) + string(sectionMenuDrawerSlot{cfg: cfg}.Render())
	if strings.Contains(fr, "Menu") {
		t.Errorf("French ctx: English Menu label still rendered in\n%s", fr)
	}
	if strings.Count(fr, "Sommaire") < 2 {
		t.Errorf("French ctx: Sommaire missing from the pill or the sheet title in\n%s", fr)
	}
}

func TestPhoneSheetCloseSpeaksCtx(t *testing.T) {
	cfg := PhoneSheetConfig{ID: "s", Title: "Menu"}
	if en := string(PhoneSheet(cfg, render.Text("body"))); !strings.Contains(en, `aria-label="Close"`) {
		t.Errorf("nil ctx: English close label missing from\n%s", en)
	}
	cfg.Ctx = frenchInteractive()
	if fr := string(PhoneSheet(cfg, render.Text("body"))); !strings.Contains(fr, `aria-label="Fermer"`) {
		t.Errorf("French ctx: close label missing from\n%s", fr)
	}
}
