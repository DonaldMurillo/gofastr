package main

import (
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/framework/ui"
)

// FrameworkUIScreen is a single live-demo page for every component
// shipped in framework/ui. It dogfoods the framework's semantic
// components — every block on this page IS the component being
// described.
type FrameworkUIScreen struct{}

func (s *FrameworkUIScreen) ScreenTitle() string        { return "Framework UI" }
func (s *FrameworkUIScreen) ScreenDescription() string  { return "Semantic components built on framework/ui/theme." }
func (s *FrameworkUIScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *FrameworkUIScreen) Render() render.HTML {
	pageHeader := ui.PageHeader(ui.PageHeaderConfig{
		Eyebrow:  "framework/ui",
		Title:    "Semantic components",
		Subtitle: "Building blocks expressing product intent — composed from core-ui primitives, styled via framework/ui/theme tokens.",
		Actions: render.Join(
			elements.Link(elements.LinkConfig{Href: "https://github.com/DonaldMurillo/gofastr/tree/main/framework/ui",
				Text: "Source", Class: "ui-button", Attrs: elements.Attrs{"rel": "external"}}),
		),
	})

	statsRow := render.Tag("div", map[string]string{"class": "demo-stat-row"},
		ui.StatCard(ui.StatCardConfig{Label: "Active", Value: "12,483", Trend: "+8.2%", Direction: ui.TrendUp}),
		ui.StatCard(ui.StatCardConfig{Label: "Errors / hr", Value: "47", Trend: "-12%", Direction: ui.TrendDown}),
		ui.StatCard(ui.StatCardConfig{Label: "Latency p99", Value: "142ms", Trend: "stable", Direction: ui.TrendFlat}),
	)

	avatarRow := render.Tag("div", map[string]string{"class": "demo-avatar-row"},
		ui.Avatar(ui.AvatarConfig{Name: "Donald Murillo"}),
		ui.Avatar(ui.AvatarConfig{Name: "Alice Thompson"}),
		ui.Avatar(ui.AvatarConfig{Name: "B", Size: "3rem"}),
		ui.Avatar(ui.AvatarConfig{Name: "Catherine"}),
	)

	badgeRow := render.Tag("div", map[string]string{"class": "demo-badge-row"},
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Active", Variant: ui.StatusSuccess}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Pending", Variant: ui.StatusWarning}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Failed", Variant: ui.StatusDanger}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Info", Variant: ui.StatusInfo}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Draft", Variant: ui.StatusNeutral}),
	)

	calloutStack := render.Tag("div", map[string]string{"style": "display:grid;gap:0.75rem"},
		ui.Callout(ui.CalloutConfig{Title: "Heads up", Variant: ui.StatusInfo},
			render.Text("Callouts are persistent inline blocks. Use Toast for ephemeral notifications.")),
		ui.Callout(ui.CalloutConfig{Title: "Saved", Variant: ui.StatusSuccess},
			render.Text("Your changes were saved successfully.")),
		ui.Callout(ui.CalloutConfig{Title: "Warning", Variant: ui.StatusWarning},
			render.Text("This action will revoke API keys for all team members.")),
		ui.Callout(ui.CalloutConfig{Title: "Cannot continue", Variant: ui.StatusDanger},
			render.Text("Two-factor authentication is required for this organization.")),
	)

	emptyState := ui.EmptyState(ui.EmptyStateConfig{
		Title:       "No customers yet",
		Description: "Once your team adds the first customer, they'll appear here. You can also import from CSV.",
		Action: render.Tag("div", map[string]string{"style": "display:flex;gap:0.5rem"},
			elements.Button(elements.ButtonConfig{Label: "Import CSV", Class: "ui-button"}),
			ui.DangerButton(ui.DangerButtonConfig{Label: "Reset all"}),
		),
	})

	form := ui.FormSection(ui.FormSectionConfig{
		Heading:     "Profile",
		Description: "Public info — visible on your account page.",
	},
		ui.FormField(ui.FormFieldConfig{
			Label: "Display name", For: "name", Required: true,
			Help:  "Shown next to your messages.",
			Input: elements.Input(elements.InputConfig{Type: "text", Name: "name", ID: "name"}),
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "Email", For: "email", Required: true,
			Error: "Please enter a valid email address.",
			Input: elements.Input(elements.InputConfig{Type: "email", Name: "email", ID: "email"}),
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "Bio", For: "bio",
			Help:  "Markdown supported. Max 500 characters.",
			Input: elements.TextArea(elements.TextAreaConfig{Name: "bio", ID: "bio"}),
		}),
	)

	return render.Tag("main", nil,
		pageHeader,

		ui.Section(ui.SectionConfig{
			Heading:     "PageHeader",
			Description: "The block at the top of this page IS the PageHeader. Eyebrow + title + subtitle + an actions slot — wired through the canonical theme tokens.",
		}),

		ui.Section(ui.SectionConfig{
			Heading:     "StatCard",
			Description: "Metric tiles — label, value, optional trend pill (Up / Down / Flat).",
		}, statsRow),

		ui.Section(ui.SectionConfig{
			Heading:     "Avatar",
			Description: "Image + initials fallback. Single-letter, single-name, multi-name all handled.",
		}, avatarRow),

		ui.Section(ui.SectionConfig{
			Heading:     "StatusBadge",
			Description: "Inline status pills — five semantic variants (success / warning / danger / info / neutral).",
		}, badgeRow),

		ui.Section(ui.SectionConfig{
			Heading:     "Callout",
			Description: "Persistent inline information blocks. Distinct from Toast (ephemeral). danger / warning callouts get role=\"alert\" automatically.",
		}, calloutStack),

		ui.Section(ui.SectionConfig{
			Heading:     "EmptyState",
			Description: "Zero-data screen with title + description + action slot. Stops every list view from re-implementing \"nothing here\".",
		}, emptyState),

		ui.Section(ui.SectionConfig{
			Heading:     "FormSection + FormField + DangerButton",
			Description: "Forms are where semantic tokens earn their keep — error states, required indicators, help text, focus rings all reference the theme.",
		}, form),

		// Boundary explanation.
		ui.Callout(ui.CalloutConfig{Title: "The boundary rule", Variant: ui.StatusInfo},
			render.Tag("p", nil, render.Text(
				"If a piece maps 1:1 to an HTML element or ARIA pattern, it belongs in core-ui. If it composes primitives to express product intent, it belongs in framework/ui.")),
			render.Tag("p", nil, render.Text(
				"Accordion = core-ui. DangerButton = framework/ui. Both are valid components — they just live at different layers.")),
		),
	)
}
