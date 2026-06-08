package ui

// D14b: tests that cfg.Ctx threads the request locale into i18nui.T
// calls inside Repeater, PasswordInput, StepWizard, and Lightbox.
//
// Run: go test ./framework/ui/... -run TestCtxLocale
//
// RED before the Ctx field is added; GREEN afterwards.

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// makeCtxWithLocale builds a ctx carrying a French translator that maps
// the given key to the given translated string.
func makeCtxWithLocale(key i18nui.Key, translated string) context.Context {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", string(key), i18n.Message{Text: translated})
	tr := i18n.NewTranslator(cat, "en")
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	return i18nui.WithTranslator(ctx, tr)
}

// TestRepeaterCtxLocaleAdd: AddLabel uses ctx locale when Ctx set.
func TestRepeaterCtxLocaleAdd(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.KeyRepeaterAdd, "Ajouter")
	out := string(Repeater(RepeaterConfig{Name: "x", Ctx: ctx}))
	if !strings.Contains(out, "Ajouter") {
		t.Fatalf("expected translated AddLabel 'Ajouter', got:\n%s", out)
	}
}

// TestRepeaterCtxLocaleRemove: RemoveLabel uses ctx locale when Ctx set.
// Provide a Template so at least one remove button is rendered.
func TestRepeaterCtxLocaleRemove(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.KeyRepeaterRemove, "Supprimer")
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
	ctx := makeCtxWithLocale(i18nui.KeyPasswordInputShow, "Afficher le mot de passe")
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
	ctx := makeCtxWithLocale(i18nui.KeyStepWizardBack, "Retour")
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
	ctx := makeCtxWithLocale(i18nui.KeyStepWizardSubmit, "Envoyer")
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
	ctx := makeCtxWithLocale(i18nui.KeyStepWizardNext, "Continuer")
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
	ctx := makeCtxWithLocale(i18nui.KeyLightboxPrev, "Image précédente")
	slot := &lightboxSlot{name: "lb", label: "x", navArrows: true, ctx: ctx}
	out := string(slot.Render())
	if !strings.Contains(out, "Image précédente") {
		t.Fatalf("expected translated Prev label, got:\n%s", out)
	}
}

// TestLightboxCtxLocaleNext: Next aria-label uses ctx locale when Ctx set.
func TestLightboxCtxLocaleNext(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.KeyLightboxNext, "Image suivante")
	slot := &lightboxSlot{name: "lb", label: "x", navArrows: true, ctx: ctx}
	out := string(slot.Render())
	if !strings.Contains(out, "Image suivante") {
		t.Fatalf("expected translated Next label, got:\n%s", out)
	}
}

// TestLightboxCtxLocaleDownload: Download aria-label uses ctx locale.
func TestLightboxCtxLocaleDownload(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.KeyLightboxDownload, "Télécharger")
	slot := &lightboxSlot{name: "lb", label: "x", allowDownload: true, ctx: ctx}
	out := string(slot.Render())
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

// TestLightboxCfgCtxPassedToSlot: LightboxConfig.Ctx flows into the
// internal lightboxSlot (verifies the wire-up in Lightbox()).
func TestLightboxCfgCtxPassedToSlot(t *testing.T) {
	ctx := makeCtxWithLocale(i18nui.KeyLightboxPrev, "Précédent")
	b := Lightbox(LightboxConfig{Name: "test", NavArrows: true, Ctx: ctx})
	// Find the body slot and render it.
	def := b.Definition()
	var html string
	for _, s := range def.Slots {
		if s.Name == "body" {
			html = string(s.Component.Render())
			break
		}
	}
	if html == "" {
		t.Fatal("Lightbox widget must have a 'body' slot that renders non-empty HTML")
	}
	if !strings.Contains(html, "Précédent") {
		t.Fatalf("LightboxConfig.Ctx must be passed into the slot; got:\n%s", html)
	}
}
