package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
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
			html.Link(html.LinkConfig{Href: "https://github.com/DonaldMurillo/gofastr/tree/main/framework/ui",
				Text: "Source", Class: "ui-button", Attrs: html.Attrs{"rel": "external"}}),
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
		ui.Avatar(ui.AvatarConfig{Name: "B", Size: ui.AvatarLg}),
		ui.Avatar(ui.AvatarConfig{Name: "Catherine"}),
	)

	badgeRow := render.Tag("div", map[string]string{"class": "demo-badge-row"},
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Active", Variant: ui.StatusSuccess}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Pending", Variant: ui.StatusWarning}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Failed", Variant: ui.StatusDanger}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Info", Variant: ui.StatusInfo}),
		ui.StatusBadge(ui.StatusBadgeConfig{Label: "Draft", Variant: ui.StatusNeutral}),
	)

	calloutStack := render.Tag("div", map[string]string{"class": "demo-stack demo-stack--sm"},
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
		Action: render.Tag("div", map[string]string{"class": "demo-row-flex"},
			html.Button(html.ButtonConfig{Label: "Import CSV", Class: "ui-button"}),
			ui.Button(ui.ButtonConfig{Label: "Reset all", Variant: ui.ButtonDanger}),
		),
	})

	form := ui.FormSection(ui.FormSectionConfig{
		Heading:     "Profile",
		Description: "Public info — visible on your account page.",
	},
		ui.FormField(ui.FormFieldConfig{
			Label: "Display name", For: "name", Required: true,
			Help:  "Shown next to your messages.",
			Input: html.Input(html.InputConfig{Type: "text", Name: "name", ID: "name"}),
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "Email", For: "email", Required: true,
			Error: "Please enter a valid email address.",
			Input: html.Input(html.InputConfig{Type: "email", Name: "email", ID: "email"}),
		}),
		ui.FormField(ui.FormFieldConfig{
			Label: "Bio", For: "bio",
			Help:  "Markdown supported. Max 500 characters.",
			Input: html.TextArea(html.TextAreaConfig{Name: "bio", ID: "bio"}),
		}),
	)

	return render.Tag("div", nil,
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

		// Deep-dive links.
		ui.Section(ui.SectionConfig{
			Heading:     "Deep-dive demos",
			Description: "Each of these has its own page with multiple states + composition notes.",
		},
			render.Tag("ul", map[string]string{"class": "doc-list"},
				render.Tag("li", nil,
					html.LinkHTML(html.LinkHTMLConfig{Href: "/framework-ui/datatable",
						Content: render.Join(
							render.Tag("strong", nil, render.Text("DataTable")),
							render.Tag("span", nil, render.Text("Sortable headers, pagination footer, empty state, ARIA-correct via core-ui html.")),
						)}),
				),
				render.Tag("li", nil,
					html.LinkHTML(html.LinkHTMLConfig{Href: "/framework-ui/form",
						Content: render.Join(
							render.Tag("strong", nil, render.Text("Form & validation")),
							render.Tag("span", nil, render.Text("FieldErrors round-trip; pristine vs validation-failed states side by side.")),
						)}),
				),
				render.Tag("li", nil,
					html.LinkHTML(html.LinkHTMLConfig{Href: "/framework-ui/notification",
						Content: render.Join(
							render.Tag("strong", nil, render.Text("Notification")),
							render.Tag("span", nil, render.Text("Styled toast row — five variants, optional dismiss link, role=alert auto-applied.")),
						)}),
				),
				render.Tag("li", nil,
					html.LinkHTML(html.LinkHTMLConfig{Href: "/framework-ui/theme",
						Content: render.Join(
							render.Tag("strong", nil, render.Text("Theme swap")),
							render.Tag("span", nil, render.Text("One-token-swap re-skin demo — pick a primary color and watch the whole page re-skin via :has().")),
						)}),
				),
				render.Tag("li", nil,
					html.LinkHTML(html.LinkHTMLConfig{Href: "/framework-ui/css-loading",
						Content: render.Join(
							render.Tag("strong", nil, render.Text("Component CSS loading")),
							render.Tag("span", nil, render.Text("Three load modes (Auto / Prewarm / Always), per-component <link> dedup, bundle endpoint — with live demos and DevTools instructions.")),
						)}),
				),
				render.Tag("li", nil,
					html.LinkHTML(html.LinkHTMLConfig{Href: "/framework-ui/themed",
						Content: render.Join(
							render.Tag("strong", nil, render.Text("Section-level theme overrides")),
							render.Tag("span", nil, render.Text("ui.Themed(ref, …) wraps a subtree with a scoped CSS variable cascade — same components, different palette.")),
						)}),
				),
				render.Tag("li", nil,
					html.LinkHTML(html.LinkHTMLConfig{Href: "/framework-ui/image-pipeline",
						Content: render.Join(
							render.Tag("strong", nil, render.Text("Image pipeline")),
							render.Tag("span", nil, render.Text("Pure-Go Resize / Rotate / Flip / Modulate / Placeholder / BlurHash — no CGo, no system libraries.")),
						)}),
				),
			),
		),

		// Boundary explanation.
		ui.Callout(ui.CalloutConfig{Title: "The boundary rule", Variant: ui.StatusInfo},
			render.Tag("p", nil, render.Text(
				"If a piece maps 1:1 to an HTML element or ARIA pattern, it belongs in core-ui. If it composes primitives to express product intent, it belongs in framework/ui.")),
			render.Tag("p", nil, render.Text(
				"Accordion = core-ui. DangerButton = framework/ui. Both are valid components — they just live at different layers.")),
		),
	)
}
