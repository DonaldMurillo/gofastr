package ui

// D14b: tests that cfg.Ctx threads the request locale into i18nui.T
// calls inside Repeater, PasswordInput and StepWizard, and that the
// mounted Lightbox slot takes the locale from the render's context.
//
// Run: go test ./framework/ui/... -run TestCtxLocale
//
// RED before the Ctx field is added; GREEN afterwards.

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// The sidebar's default nav name speaks the request's language.
func TestSidebarDefaultNavLabelLocalized(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.NavPrimary, "Principal")
	out, err := component.SafeRenderCtx(ctx, Sidebar(SidebarConfig{Items: []SidebarItem{{Label: "Home", Href: "/"}}}))
	if err != nil {
		t.Fatalf("sidebar render: %v", err)
	}
	if !strings.Contains(string(out), `aria-label="Principal"`) || strings.Contains(string(out), `aria-label="Primary"`) {
		t.Errorf("the default nav name stayed English:\n%s", out)
	}
}

// makeCtxWithLocale builds a ctx carrying a French translator that maps
// the given key to the given translated string.
func makeCtxWithLocale(key i18n.Phrase, translated string) context.Context {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", key.Key(), i18n.Message{Text: translated})
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	return i18n.WithTranslator(ctx, tr)
}

// TestRepeaterCtxLocaleAdd: AddLabel uses ctx locale when Ctx set.
func TestRepeaterCtxLocaleAdd(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.RepeaterAdd, "Ajouter")
	out := string(Repeater(RepeaterConfig{Name: "x", Ctx: ctx}))
	if !strings.Contains(out, "Ajouter") {
		t.Fatalf("expected translated AddLabel 'Ajouter', got:\n%s", out)
	}
}

// TestRepeaterCtxLocaleRemove: RemoveLabel uses ctx locale when Ctx set.
// Provide a Template so at least one remove button is rendered.
func TestRepeaterCtxLocaleRemove(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.RepeaterRemove, "Supprimer")
	out := string(Repeater(RepeaterConfig{
		Name: "x",
		Ctx:  ctx,
		Template: func(i int) render.HTML {
			return render.Text("item")
		},
	}))
	if !strings.Contains(out, "Supprimer") {
		t.Fatalf("expected translated RemoveLabel 'Supprimer', got:\n%s", out)
	}
}

// TestRepeaterNilCtxPreservesToday: no Ctx = English default (compat).
func TestRepeaterNilCtxPreservesToday(t *testing.T) {
	out := string(Repeater(RepeaterConfig{Name: "x"}))
	if !strings.Contains(out, "Add item") {
		t.Fatalf("nil Ctx should fall back to English default, got:\n%s", out)
	}
}

// TestPasswordInputCtxLocale: show-password aria-label uses ctx locale.
func TestPasswordInputCtxLocale(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.PasswordInputShow, "Afficher le mot de passe")
	out := string(PasswordInput(PasswordInputConfig{
		Name: "pw",
		ID:   "pw",
		Ctx:  ctx,
	}))
	if !strings.Contains(out, "Afficher le mot de passe") {
		t.Fatalf("expected translated aria-label, got:\n%s", out)
	}
}

// TestPasswordInputNilCtxPreservesToday: no Ctx = English default (compat).
func TestPasswordInputNilCtxPreservesToday(t *testing.T) {
	out := string(PasswordInput(PasswordInputConfig{Name: "pw", ID: "pw"}))
	if !strings.Contains(out, "Show password") {
		t.Fatalf("nil Ctx should fall back to English default, got:\n%s", out)
	}
}

// TestStepWizardCtxLocaleBack: Back button uses ctx locale.
func TestStepWizardCtxLocaleBack(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.StepWizardBack, "Retour")
	out := string(StepWizard(StepWizardConfig{
		Steps:       []StepWizardStep{{Heading: "A"}, {Heading: "B"}},
		CurrentStep: 1,
		Action:      "/wiz",
		Ctx:         ctx,
	}))
	if !strings.Contains(out, "Retour") {
		t.Fatalf("expected translated Back 'Retour', got:\n%s", out)
	}
}

// TestStepWizardCtxLocaleSubmit: Submit button uses ctx locale.
func TestStepWizardCtxLocaleSubmit(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.StepWizardSubmit, "Envoyer")
	out := string(StepWizard(StepWizardConfig{
		Steps:       []StepWizardStep{{Heading: "A"}, {Heading: "B"}},
		CurrentStep: 1,
		Action:      "/wiz",
		Ctx:         ctx,
	}))
	if !strings.Contains(out, "Envoyer") {
		t.Fatalf("expected translated Submit 'Envoyer', got:\n%s", out)
	}
}

// TestStepWizardCtxLocaleNext: Next button uses ctx locale on non-final step.
func TestStepWizardCtxLocaleNext(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.StepWizardNext, "Continuer")
	out := string(StepWizard(StepWizardConfig{
		Steps:       []StepWizardStep{{Heading: "A"}, {Heading: "B"}, {Heading: "C"}},
		CurrentStep: 0,
		Action:      "/wiz",
		Ctx:         ctx,
	}))
	if !strings.Contains(out, "Continuer") {
		t.Fatalf("expected translated Next 'Continuer', got:\n%s", out)
	}
}

// TestStepWizardNilCtxPreservesToday: no Ctx = English default (compat).
func TestStepWizardNilCtxPreservesToday(t *testing.T) {
	out := string(StepWizard(StepWizardConfig{
		Steps:  []StepWizardStep{{Heading: "A"}},
		Action: "/wiz",
	}))
	if !strings.Contains(out, "Submit") {
		t.Fatalf("nil Ctx should fall back to English default, got:\n%s", out)
	}
}

// TestLightboxCtxLocalePrev: Prev aria-label uses ctx locale when Ctx set.
func TestLightboxCtxLocalePrev(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.LightboxPrev, "Image précédente")
	slot := &lightboxSlot{name: "lb", label: "x", navArrows: true}
	out := string(slot.RenderCtx(ctx))
	if !strings.Contains(out, "Image précédente") {
		t.Fatalf("expected translated Prev label, got:\n%s", out)
	}
}

// TestLightboxCtxLocaleNext: Next aria-label uses ctx locale when Ctx set.
func TestLightboxCtxLocaleNext(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.LightboxNext, "Image suivante")
	slot := &lightboxSlot{name: "lb", label: "x", navArrows: true}
	out := string(slot.RenderCtx(ctx))
	if !strings.Contains(out, "Image suivante") {
		t.Fatalf("expected translated Next label, got:\n%s", out)
	}
}

// TestLightboxCtxLocaleDownload: Download aria-label uses ctx locale.
func TestLightboxCtxLocaleDownload(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.LightboxDownload, "Télécharger")
	slot := &lightboxSlot{name: "lb", label: "x", allowDownload: true}
	out := string(slot.RenderCtx(ctx))
	if !strings.Contains(out, "Télécharger") {
		t.Fatalf("expected translated Download label, got:\n%s", out)
	}
}

// TestLightboxNilCtxPreservesToday: no Ctx = English default (compat).
func TestLightboxNilCtxPreservesToday(t *testing.T) {
	slot := &lightboxSlot{name: "lb", label: "x", navArrows: true, allowDownload: true}
	out := string(slot.Render())
	if !strings.Contains(out, "Previous image") {
		t.Fatalf("nil ctx should fall back to English defaults, got:\n%s", out)
	}
}

// TestLightboxSlotUsesRequestCtx: the mounted body slot takes its
// locale from the render's context (verifies the wire-up in Lightbox()).
func TestLightboxSlotUsesRequestCtx(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.LightboxPrev, "Précédent")
	b := Lightbox(LightboxConfig{Name: "test", NavArrows: true})
	// Find the body slot and render it.
	def := b.Definition()
	var html string
	for _, s := range def.Slots {
		if s.Name == "body" {
			out, err := component.SafeRenderCtx(ctx, s.Component)
			if err != nil {
				t.Fatal(err)
			}
			html = string(out)
			break
		}
	}
	if html == "" {
		t.Fatal("Lightbox widget must have a 'body' slot that renders non-empty HTML")
	}
	if !strings.Contains(html, "Précédent") {
		t.Fatalf("the slot must render in the request's locale; got:\n%s", html)
	}
}
