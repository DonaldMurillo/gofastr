package ui

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
)

func mustContain(t *testing.T, h render.HTML, sub string) {
	t.Helper()
	if !strings.Contains(string(h), sub) {
		t.Fatalf("expected HTML to contain %q\ngot: %s", sub, h)
	}
}

// ─── PageHeader ───
func TestPageHeaderRequiresTitle(t *testing.T) {
	defer func() { recover() }()
	PageHeader(PageHeaderConfig{})
	t.Fatal("expected panic with empty Title")
}

func TestPageHeaderRendersTitleAndOptionalParts(t *testing.T) {
	h := PageHeader(PageHeaderConfig{
		Title:    "Customers",
		Subtitle: "1,283 active",
		Eyebrow:  "Admin",
		Actions:  render.Text("ACTIONS_SLOT"),
	})
	for _, want := range []string{"Customers", "1,283 active", "Admin", "ACTIONS_SLOT",
		"ui-page-header", "ui-page-header__eyebrow", "ui-page-header__actions"} {
		mustContain(t, h, want)
	}
}

func TestPageHeaderOmitsActionsWhenEmpty(t *testing.T) {
	h := PageHeader(PageHeaderConfig{Title: "x"})
	if strings.Contains(string(h), "ui-page-header__actions") {
		t.Fatal("expected no actions div when Actions is empty")
	}
}

// ─── Section ───
func TestSectionRendersHeadingDescriptionBody(t *testing.T) {
	h := Section(SectionConfig{Heading: "Settings", Description: "Account-wide"},
		render.Text("BODY"))
	for _, want := range []string{"Settings", "Account-wide", "BODY", "ui-section__body"} {
		mustContain(t, h, want)
	}
}

// ─── FormField ───
func TestFormFieldRequiresLabelForInput(t *testing.T) {
	defer func() { recover() }()
	FormField(FormFieldConfig{})
	t.Fatal("expected panic on empty config")
}

func TestFormFieldRequired(t *testing.T) {
	in := elements.Input(elements.InputConfig{Type: "text", Name: "n", ID: "name"})
	h := FormField(FormFieldConfig{
		Label: "Name", For: "name", Required: true, Input: in,
	})
	mustContain(t, h, `for="name"`)
	mustContain(t, h, "Name")
	mustContain(t, h, "ui-form-field__required")
}

func TestFormFieldErrorSwitchesStyling(t *testing.T) {
	in := elements.Input(elements.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{
		Label: "Name", For: "n", Error: "Required field", Input: in,
	})
	mustContain(t, h, "is-error")
	mustContain(t, h, `role="alert"`)
	mustContain(t, h, "Required field")
	if strings.Contains(string(h), "ui-form-field__help") {
		t.Fatal("expected help to be hidden when Error set")
	}
}

func TestFormFieldHelpRendersWhenNoError(t *testing.T) {
	in := elements.Input(elements.InputConfig{Type: "text", Name: "n", ID: "n"})
	h := FormField(FormFieldConfig{Label: "x", For: "n", Help: "Hint", Input: in})
	mustContain(t, h, "Hint")
	mustContain(t, h, "ui-form-field__help")
}

// ─── DangerButton ───
func TestDangerButtonHasDangerVariantClass(t *testing.T) {
	h := DangerButton(DangerButtonConfig{Label: "Delete"})
	mustContain(t, h, "ui-button--danger")
	mustContain(t, h, "Delete")
}

// ─── StatusBadge ───
func TestStatusBadgeVariantsRenderClass(t *testing.T) {
	for _, v := range []StatusVariant{StatusSuccess, StatusWarning, StatusDanger, StatusInfo, StatusNeutral} {
		h := StatusBadge(StatusBadgeConfig{Label: "x", Variant: v})
		want := "ui-badge--" + string(v)
		mustContain(t, h, want)
	}
}

func TestStatusBadgeDefaultsToNeutral(t *testing.T) {
	h := StatusBadge(StatusBadgeConfig{Label: "x"})
	mustContain(t, h, "ui-badge--neutral")
}

// ─── EmptyState ───
func TestEmptyStateRendersTitleDescriptionAction(t *testing.T) {
	h := EmptyState(EmptyStateConfig{
		Title: "No customers yet", Description: "Invite your first.",
		Action: render.Text("INVITE_BUTTON"),
	})
	for _, want := range []string{"No customers yet", "Invite your first.", "INVITE_BUTTON",
		"ui-empty-state__action"} {
		mustContain(t, h, want)
	}
}

// ─── Callout ───
func TestCalloutRoleSwitchesForAlerts(t *testing.T) {
	for _, v := range []StatusVariant{StatusDanger, StatusWarning} {
		h := Callout(CalloutConfig{Title: "x", Variant: v}, render.Text("body"))
		mustContain(t, h, `role="alert"`)
	}
	for _, v := range []StatusVariant{StatusInfo, StatusSuccess, StatusNeutral} {
		h := Callout(CalloutConfig{Title: "x", Variant: v}, render.Text("body"))
		mustContain(t, h, `role="note"`)
	}
}

// ─── StatCard ───
func TestStatCardRequiresLabelAndValue(t *testing.T) {
	defer func() { recover() }()
	StatCard(StatCardConfig{Label: "x"})
	t.Fatal("expected panic when Value missing")
}

func TestStatCardTrendDirection(t *testing.T) {
	h := StatCard(StatCardConfig{Label: "Revenue", Value: "$12.4k", Trend: "+8%", Direction: TrendUp})
	mustContain(t, h, "ui-stat-card__trend--up")
}

// ─── Avatar ───
func TestAvatarFallsBackToInitials(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "Donald Murillo"})
	mustContain(t, h, "DM")
	mustContain(t, h, "ui-avatar__initials")
}

func TestAvatarUsesImageWhenSrcSet(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "Alice", Src: "/avatars/alice.png"})
	mustContain(t, h, `src="/avatars/alice.png"`)
	mustContain(t, h, `alt="Alice"`)
}

func TestAvatarSizeStyle(t *testing.T) {
	h := Avatar(AvatarConfig{Name: "x", Size: "3rem"})
	mustContain(t, h, "inline-size:3rem")
	mustContain(t, h, "block-size:3rem")
}

func TestInitialsHelper(t *testing.T) {
	cases := map[string]string{
		"Donald Murillo": "DM",
		"alice":          "A",
		"three name foo": "TF",
		"":               "",
	}
	for in, want := range cases {
		got := initials(in)
		if got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}
