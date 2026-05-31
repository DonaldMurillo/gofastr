package main

// =============================================================================
// /components — one entry per framework/ui + core-ui/patterns primitive.
// Each entry has:
//   - Slug      — route segment (/components/<slug>)
//   - Name      — display name
//   - Category  — coarse grouping for the index page
//   - Desc      — one-line role
//   - Demo()    — render.HTML showing the primitive live, configured with
//                 sensible defaults so the page works without setup
//
// The catalog is the single source of truth: main.go iterates over it to
// register routes, ComponentsIndexScreen iterates over it to render cards,
// and ComponentShowcaseScreen iterates to look up the active entry.
//
// Where a component requires non-trivial backend wiring (DataTable's RPC
// island, FileUpload's storage backend, ConfirmAction's RPC handler), the
// Demo renders a smaller stand-alone variant or a static markup mock so
// every page has SOMETHING that works. Comments call out the simplification.
// =============================================================================

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	patternsAccordion "github.com/DonaldMurillo/gofastr/core-ui/patterns/accordion"
	patternsBreadcrumbs "github.com/DonaldMurillo/gofastr/core-ui/patterns/breadcrumbs"
	patternsPagination "github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// componentEntry — one component in the catalog.
type componentEntry struct {
	Slug     string
	Name     string
	Category string
	Desc     string
	Demo     func() render.HTML
}

// noteOnlyComponents are components whose showcase shows an explanatory
// NOTE instead of a live, interactive demo — they need per-page backend
// wiring (an RPC, a mounted widget, image sources), so a self-contained
// live render isn't possible. Their stage is labeled "Note", not "Live",
// so the box doesn't claim to be something it isn't.
var noteOnlyComponents = map[string]bool{
	"datatable": true, "combobox": true, "multiselect": true,
	"conditionalfield": true, "formrepeater": true, "repeater": true,
	"gallery": true, "lightbox": true, "commandpalette": true,
	"globalsearch": true, "notificationbell": true, "pipelineimage": true,
	"confirmaction": true,
}

// componentPkg returns the Go source package for a component, used to
// link the showcase header at its API docs on pkg.go.dev. Most live in
// framework/ui; a few are core-ui patterns or the image pipeline.
func componentPkg(slug string) string {
	switch slug {
	case "accordion", "breadcrumbs", "pagination":
		return "core-ui/patterns/" + slug
	case "image", "pipelineimage":
		return "framework/image"
	default:
		return "framework/ui"
	}
}

// componentCatalog — every component the site showcases. Grouped by
// category for ComponentsIndexScreen; routes are flat at /components/<slug>.
var componentCatalog = []componentEntry{
	// ---------- Buttons & links ----------
	{"button", "Button", "Buttons & links", "Primary action element with size + variant slots.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.Button(ui.ButtonConfig{Label: "Primary", Variant: ui.ButtonPrimary}),
			ui.Button(ui.ButtonConfig{Label: "Secondary", Variant: ui.ButtonSecondary}),
			ui.Button(ui.ButtonConfig{Label: "Ghost", Variant: ui.ButtonGhost}),
			ui.Button(ui.ButtonConfig{Label: "Danger", Variant: ui.ButtonDanger}),
		)
	}},
	{"link", "Link", "Buttons & links", "Typed anchor with external-link affordances.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.Link(ui.LinkConfig{Href: "/docs/", Text: "Internal link"}),
			ui.Link(ui.LinkConfig{
				Href:       "https://pkg.go.dev/",
				Text:       "External link",
				ExtraAttrs: html.Attrs{"target": "_blank", "rel": "noopener"},
			}),
		)
	}},
	{"copybutton", "CopyButton", "Buttons & links", "Copies text from a target selector to clipboard.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-stack"},
			html.Div(html.DivConfig{ID: "copy-demo-source"}, render.Text("hello world")),
			ui.CopyButton(ui.CopyButtonConfig{Target: "#copy-demo-source", Label: "Copy hello"}),
		)
	}},
	{"shortcuthint", "ShortcutHint", "Buttons & links", "Inline keyboard-shortcut chip.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Mod+K"}),
			ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Shift+/"}),
		)
	}},
	{"themetoggle", "ThemeToggle", "Buttons & links", "Cycles data-color-scheme between dark/light/auto.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleIcon}),
			ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeToggleLabel}),
			ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeTogglePill}),
		)
	}},

	// ---------- Tags & badges ----------
	{"tag", "Tag", "Tags & badges", "Compact status pill, optionally dismissable.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.Tag(ui.TagConfig{Label: "neutral"}),
			ui.Tag(ui.TagConfig{Label: "success", Variant: ui.StatusSuccess}),
			ui.Tag(ui.TagConfig{Label: "warning", Variant: ui.StatusWarning}),
			ui.Tag(ui.TagConfig{Label: "danger", Variant: ui.StatusDanger}),
			ui.Tag(ui.TagConfig{Label: "info", Variant: ui.StatusInfo}),
		)
	}},
	{"statusbadge", "StatusBadge", "Tags & badges", "Inline dot + label status indicator.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.StatusBadge(ui.StatusBadgeConfig{Label: "Online", Variant: ui.StatusSuccess}),
			ui.StatusBadge(ui.StatusBadgeConfig{Label: "Degraded", Variant: ui.StatusWarning}),
			ui.StatusBadge(ui.StatusBadgeConfig{Label: "Offline", Variant: ui.StatusDanger}),
		)
	}},

	// ---------- Feedback / surfaces ----------
	{"banner", "Banner", "Feedback", "Full-width alert; optional dismiss + action.", func() render.HTML {
		return ui.Banner(ui.BannerConfig{
			Title:   "Pre-alpha",
			Body:    "GoFastr is pre-alpha — APIs change between commits.",
			Variant: ui.BannerWarn,
		})
	}},
	{"callout", "Callout", "Feedback", "Bordered prose call-out for tips or warnings.", func() render.HTML {
		return ui.Callout(ui.CalloutConfig{Title: "Heads up", Variant: ui.StatusInfo},
			render.Text("This component is a thin wrapper over <aside> with a left accent rule."),
		)
	}},
	{"notification", "Notification", "Feedback", "Toast-style notification with icon + variant.", func() render.HTML {
		return ui.Notification(ui.NotificationConfig{Title: "Saved", Body: "Your changes are persisted.", Variant: ui.StatusSuccess})
	}},
	{"emptystate", "EmptyState", "Feedback", "Zero-data placeholder with optional CTA.", func() render.HTML {
		return ui.EmptyState(ui.EmptyStateConfig{
			Title:       "No posts yet",
			Description: "Create your first post to see it here.",
			Action:      ui.Button(ui.ButtonConfig{Label: "New post", Variant: ui.ButtonPrimary}),
		})
	}},
	{"spinner", "Spinner", "Feedback", "Indeterminate progress indicator.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.Spinner(ui.SpinnerConfig{}),
			ui.Spinner(ui.SpinnerConfig{Size: ui.SpinnerLg}),
		)
	}},
	{"skeleton", "SkeletonPresets", "Feedback", "Shimmer placeholders while content loads.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-stack"},
			ui.SkeletonAvatar(ui.SkeletonAvatarConfig{}),
			ui.SkeletonRow(ui.SkeletonRowConfig{}),
			ui.SkeletonRow(ui.SkeletonRowConfig{}),
			ui.SkeletonCard(ui.SkeletonCardConfig{}),
		)
	}},
	{"pollingindicator", "PollingIndicator", "Feedback", "Animated live-data heartbeat.", func() render.HTML {
		return ui.PollingIndicator(ui.PollingIndicatorConfig{Label: "Live"})
	}},
	{"networkretrybanner", "NetworkRetryBanner", "Feedback", "Surface that appears when an island RPC fails.", func() render.HTML {
		return ui.NetworkRetryBanner(ui.NetworkRetryBannerConfig{HealthEndpoint: "/__gofastr/health"})
	}},

	// ---------- Layout ----------
	{"card", "Card", "Layout", "Surface with optional header / footer.", func() render.HTML {
		return ui.Card(ui.CardConfig{
			Heading:     "A typical card",
			Description: "Header + body + footer slots; theme-skinned automatically.",
			Footer:      html.Div(html.DivConfig{Class: "demo-row"}, ui.Button(ui.ButtonConfig{Label: "Action", Variant: ui.ButtonPrimary})),
		}, render.Tag("p", nil, render.Text("This is the body. The card's surface, border, and radius come from the theme.")))
	}},
	{"container", "Container", "Layout", "Max-width wrapper with width tokens.", func() render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow},
			render.Tag("p", nil, render.Text("Narrow container — text columns stay readable.")),
		)
	}},
	{"stack", "Stack", "Layout", "Vertical flex stack with gap token.", func() render.HTML {
		return ui.Stack(ui.StackConfig{Gap: ui.GapLG},
			render.Tag("div", map[string]string{"class": "fact"}, render.Text("Top")),
			render.Tag("div", map[string]string{"class": "fact"}, render.Text("Middle")),
			render.Tag("div", map[string]string{"class": "fact"}, render.Text("Bottom")),
		)
	}},
	{"grid", "Grid", "Layout", "CSS Grid with min column width + gap tokens.", func() render.HTML {
		return ui.Grid(ui.GridConfig{Min: "12rem", Gap: ui.GapMD},
			render.Tag("div", map[string]string{"class": "fact"}, render.Text("Cell 1")),
			render.Tag("div", map[string]string{"class": "fact"}, render.Text("Cell 2")),
			render.Tag("div", map[string]string{"class": "fact"}, render.Text("Cell 3")),
		)
	}},
	{"cluster", "Cluster", "Layout", "Horizontal flex with wrap.", func() render.HTML {
		return ui.Cluster(ui.ClusterConfig{Gap: ui.GapMD},
			ui.Tag(ui.TagConfig{Label: "Go"}),
			ui.Tag(ui.TagConfig{Label: "SQL"}),
			ui.Tag(ui.TagConfig{Label: "MCP"}),
			ui.Tag(ui.TagConfig{Label: "Markdown"}),
		)
	}},
	{"center", "Center", "Layout", "Centers children horizontally + vertically.", func() render.HTML {
		return ui.Center(ui.CenterConfig{MinHeight: "viewport"},
			render.Tag("p", nil, render.Text("This text is centered.")),
		)
	}},
	{"box", "Box", "Layout", "Polymorphic <div> with padding/surface tokens.", func() render.HTML {
		return ui.Box(ui.BoxConfig{Pad: ui.BoxPadLG, Surface: true},
			render.Text("Box with padding-lg + surface background."),
		)
	}},
	{"divider", "Divider", "Layout", "Horizontal or vertical rule.", func() render.HTML {
		return html.Div(html.DivConfig{},
			render.Tag("p", nil, render.Text("Above the line")),
			ui.Divider(ui.DividerConfig{}),
			render.Tag("p", nil, render.Text("Below the line")),
		)
	}},
	{"aspectratio", "AspectRatio", "Layout", "Maintains aspect ratio for media boxes.", func() render.HTML {
		return ui.AspectRatioComponent(ui.AspectRatioConfig{Ratio: ui.AspectRatio16_9},
			render.Tag("div", map[string]string{"class": "fact full"}, render.Text("16:9 box")),
		)
	}},
	{"sticky", "Sticky", "Layout", "Sticky-positioned wrapper.", func() render.HTML {
		return ui.Sticky(ui.StickyConfig{Edge: ui.StickyTop, Offset: ui.StickyOffsetLg},
			render.Tag("div", map[string]string{"class": "fact"}, render.Text("Stick scroll past me")),
		)
	}},

	// ---------- Navigation ----------
	{"pageheader", "PageHeader", "Navigation", "Eyebrow + title + actions.", func() render.HTML {
		return ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "Settings",
			Title:    "Workspace settings",
			Subtitle: "Tune defaults for everyone on this workspace.",
			Actions:  ui.Button(ui.ButtonConfig{Label: "Save changes", Variant: ui.ButtonPrimary}),
		})
	}},
	{"breadcrumbs", "Breadcrumbs", "Navigation", "Hierarchy trail.", func() render.HTML {
		return patternsBreadcrumbs.New(patternsBreadcrumbs.Config{},
			patternsBreadcrumbs.Crumb{Text: "Docs", Href: "/docs/"},
			patternsBreadcrumbs.Crumb{Text: "Modeling", Href: "/docs/#modeling"},
			patternsBreadcrumbs.Crumb{Text: "Entities"},
		)
	}},
	{"pagination", "Pagination", "Navigation", "Page-cursor controls.", func() render.HTML {
		return patternsPagination.New(patternsPagination.Config{
			Current:     2,
			Total:       8,
			HrefPattern: "?page=%d",
		})
	}},
	{"toolbar", "Toolbar", "Navigation", "Horizontal action group with separators.", func() render.HTML {
		return ui.Toolbar(ui.ToolbarConfig{
			Label: "Demo toolbar",
			Groups: []ui.ToolbarGroup{
				{Label: "Text", Children: []render.HTML{
					ui.Button(ui.ButtonConfig{Label: "Bold"}),
					ui.Button(ui.ButtonConfig{Label: "Italic"}),
					ui.Button(ui.ButtonConfig{Label: "Underline"}),
				}},
				{Label: "Insert", Children: []render.HTML{
					ui.Button(ui.ButtonConfig{Label: "Link"}),
				}},
			},
		})
	}},
	{"sidebar", "Sidebar", "Navigation", "Hierarchical navigation sidebar.", func() render.HTML {
		return ui.Sidebar(ui.SidebarConfig{
			Title: "Docs",
			Items: []ui.SidebarItem{
				{Label: "Modeling", Children: []ui.SidebarItem{
					{Label: "Entities", Href: "/docs/entities"},
					{Label: "Fields", Href: "/docs/fields"},
				}},
				{Label: "Serving", Children: []ui.SidebarItem{
					{Label: "Router", Href: "/docs/router"},
					{Label: "Middleware", Href: "/docs/middleware"},
				}},
			},
		}).Render()
	}},
	{"toc", "TableOfContents", "Navigation", "In-page anchor list (runtime fills from headings).", func() render.HTML {
		return ui.TableOfContents(ui.TOCConfig{Target: "main", Sticky: true})
	}},
	{"backtotop", "BackToTop", "Navigation", "Floating back-to-top button.", func() render.HTML {
		return ui.BackToTop(ui.BackToTopConfig{})
	}},
	{"skiplink", "SkipLink", "Navigation", "Skip-nav for assistive tech.", func() render.HTML {
		return ui.SkipLink(ui.SkipLinkConfig{})
	}},
	{"menu", "Menu", "Navigation", "Dropdown menu list.", func() render.HTML {
		return ui.Menu(ui.MenuConfig{
			Items: []ui.MenuItem{
				{Label: "Profile", Href: "/profile"},
				{Label: "Settings", Href: "/settings"},
				{Label: "Sign out", Href: "/logout"},
			},
		})
	}},
	{"segmented", "SegmentedControl", "Navigation", "Single-select tabbed buttons.", func() render.HTML {
		return ui.SegmentedControl(ui.SegmentedControlConfig{
			Name:     "demo-period",
			Label:    "Period",
			Selected: "day",
			Options: []ui.SegmentedOption{
				{Label: "Day", Value: "day"},
				{Label: "Week", Value: "week"},
				{Label: "Month", Value: "month"},
			},
		})
	}},
	{"tabs", "Tabs", "Navigation", "Native <details>-based tabs (zero JS).", func() render.HTML {
		// patterns/tabs uses Group + Stack; we render a tiny tab strip
		// using <details> to honor the CSP-friendly pattern.
		return render.Tag("div", map[string]string{"class": "demo-stack"},
			render.Tag("details", map[string]string{"open": "true"},
				render.Tag("summary", nil, render.Text("Tab A")),
				render.Tag("div", nil, render.Text("Content for tab A.")),
			),
			render.Tag("details", nil,
				render.Tag("summary", nil, render.Text("Tab B")),
				render.Tag("div", nil, render.Text("Content for tab B.")),
			),
		)
	}},

	// ---------- Disclosure ----------
	{"accordion", "Accordion", "Disclosure", "Native <details> accordion stack.", func() render.HTML {
		return patternsAccordion.Stack(patternsAccordion.StackConfig{},
			patternsAccordion.Item{Summary: "What is an entity?", Content: render.Tag("p", nil, render.Text("A typed declaration the framework turns into SQL + REST + MCP + Go."))},
			patternsAccordion.Item{Summary: "How are migrations stored?", Content: render.Tag("p", nil, render.Text("Plain SQL up/down files under .gofastr/migrations/."))},
			patternsAccordion.Item{Summary: "Can agents drop tables?", Content: render.Tag("p", nil, render.Text("Only with an approved plan — see /kiln."))},
		)
	}},
	{"tooltip", "Tooltip", "Disclosure", "Hover/focus-triggered tip.", func() render.HTML {
		return ui.Tooltip(ui.TooltipConfig{Text: "This is a tooltip"},
			ui.Button(ui.ButtonConfig{Label: "Hover me"}),
		)
	}},
	{"confirmaction", "ConfirmAction", "Disclosure", "Two-step confirm interaction (trigger + modal pair).", func() render.HTML {
		// ConfirmAction returns (trigger, *widget.Builder) — the modal
		// needs to be mounted once at app startup. The showcase renders
		// the trigger plus a static explanation; the modal isn't wired
		// here because it'd require widget.Mount in main.go.
		trigger, _ := ui.ConfirmAction(ui.ConfirmActionConfig{
			Name:         "demo-confirm",
			TriggerLabel: "Delete record",
			Title:        "Delete record?",
			Body:         "This permanently removes the record.",
			RPCPath:      "#",
		})
		return html.Div(html.DivConfig{Class: "demo-stack"},
			trigger,
			html.Div(html.DivConfig{Class: "fact"},
				render.Text("ConfirmAction returns a trigger HTML + a modal builder; mount the modal once at app startup via widget.Mount."),
			),
		)
	}},

	// ---------- Forms ----------
	{"form", "Form", "Forms", "Form container with submit + validation.", func() render.HTML {
		emailInput := render.Tag("input", map[string]string{
			"type": "email", "name": "email", "id": "demo-email", "required": "",
		})
		pwInput := render.Tag("input", map[string]string{
			"type": "password", "name": "password", "id": "demo-password", "required": "",
		})
		return ui.Form(ui.FormConfig{Action: "#", Method: "POST", SubmitLabel: "Sign in"},
			ui.FormField(ui.FormFieldConfig{Label: "Email", For: "demo-email", Required: true, Input: emailInput}),
			ui.FormField(ui.FormFieldConfig{Label: "Password", For: "demo-password", Required: true, Input: pwInput}),
		)
	}},
	{"formfield", "FormField", "Forms", "Label + input + help text + error.", func() render.HTML {
		input := render.Tag("input", map[string]string{"type": "text", "name": "name", "id": "demo-name"})
		return ui.FormField(ui.FormFieldConfig{
			Label: "Display name", For: "demo-name",
			Help:  "Visible to everyone in your workspace.",
			Input: input,
		})
	}},
	{"formsection", "FormSection", "Forms", "Bordered group of related fields.", func() render.HTML {
		firstIn := render.Tag("input", map[string]string{"type": "text", "name": "first", "id": "demo-first"})
		lastIn := render.Tag("input", map[string]string{"type": "text", "name": "last", "id": "demo-last"})
		return ui.FormSection(ui.FormSectionConfig{Heading: "Profile", Description: "Tell us a little about you."},
			ui.FormField(ui.FormFieldConfig{Label: "First name", For: "demo-first", Input: firstIn}),
			ui.FormField(ui.FormFieldConfig{Label: "Last name", For: "demo-last", Input: lastIn}),
		)
	}},
	{"select", "Select", "Forms", "Native <select> styled to match the theme.", func() render.HTML {
		return ui.Select(ui.SelectConfig{
			Name:    "country",
			Label:   "Country",
			Options: []ui.SelectOption{{Text: "Mexico", Value: "mx"}, {Text: "Canada", Value: "ca"}, {Text: "USA", Value: "us"}},
		})
	}},
	{"checkbox", "Checkbox", "Forms", "Single boolean toggle.", func() render.HTML {
		return ui.Checkbox(ui.ToggleConfig{Name: "ok", Label: "Subscribe to release notes"})
	}},
	{"checkboxgroup", "CheckboxGroup", "Forms", "Grouped boolean options.", func() render.HTML {
		return ui.CheckboxGroup(ui.CheckboxGroupConfig{
			Legend: "Frameworks you use",
			Name:   "frameworks",
			Options: []ui.CheckboxGroupOption{
				{Label: "GoFastr", Value: "gofastr"},
				{Label: "Next.js", Value: "next"},
				{Label: "Phoenix", Value: "phoenix"},
			},
		})
	}},
	{"radio", "Radio", "Forms", "Single-select among grouped options.", func() render.HTML {
		return ui.RadioGroup(ui.RadioGroupConfig{
			Legend: "Notification frequency",
			Name:   "freq",
			Options: []ui.RadioGroupOption{
				{Label: "Always", Value: "all"},
				{Label: "Mentions only", Value: "mention"},
				{Label: "Never", Value: "none"},
			},
		})
	}},
	{"switch", "Switch", "Forms", "On/off toggle that looks like a physical switch.", func() render.HTML {
		return ui.Switch(ui.ToggleConfig{Name: "live", Label: "Live updates"})
	}},
	{"textarea", "Textarea", "Forms", "Multi-line text input with autosize.", func() render.HTML {
		return ui.TextArea(ui.TextAreaConfig{Name: "body", Label: "Body", Placeholder: "Write your post…", Rows: 6})
	}},
	{"numberinput", "NumberInput", "Forms", "Numeric input with stepper buttons.", func() render.HTML {
		return ui.NumberInput(ui.NumberInputConfig{Name: "qty", Label: "Quantity", Min: 0, Max: 99, Value: 1})
	}},
	{"passwordinput", "PasswordInput", "Forms", "Password with show/hide toggle.", func() render.HTML {
		return ui.PasswordInput(ui.PasswordInputConfig{Name: "pw", ID: "demo-pw"})
	}},
	{"searchinput", "SearchInput", "Forms", "Search field with leading icon + clear button.", func() render.HTML {
		return ui.SearchInput(ui.SearchInputConfig{Name: "q", ID: "demo-search", Placeholder: "Search docs…"})
	}},
	{"rangeslider", "RangeSlider", "Forms", "Min/max thumb pair.", func() render.HTML {
		return ui.RangeSlider(ui.RangeSliderConfig{Name: "price", Label: "Price range", Min: 0, Max: 1000, ValueLow: 100, ValueHigh: 700, ShowValue: true})
	}},
	{"slider", "Slider", "Forms", "Single-value slider.", func() render.HTML {
		return ui.Slider(ui.SliderConfig{Name: "vol", Label: "Volume", Min: 0, Max: 100, Value: 50, ShowValue: true})
	}},
	{"taginput", "TagInput", "Forms", "Free-form tag entry with chips.", func() render.HTML {
		return ui.TagInput(ui.TagInputConfig{Name: "tags", Label: "Tags", Values: []string{"go", "framework", "agent"}})
	}},
	{"combobox", "Combobox", "Forms", "Type-ahead suggestion picker.", func() render.HTML {
		// Static demo since wiring the search RPC island is per-page.
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("Combobox needs an RPC search endpoint. See /docs/components for the wiring recipe."),
		)
	}},
	{"multiselect", "Multiselect", "Forms", "Multi-pick from a list with chips.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("Multiselect compounds Combobox with an RPC. Demo deferred to its own integration page."),
		)
	}},
	{"filterchipbar", "FilterChipBar", "Forms", "Active filter chip strip with per-chip dismiss RPC.", func() render.HTML {
		return ui.FilterChipBar(ui.FilterChipBarConfig{
			Filters: []ui.FilterChip{
				{Label: "Open", DismissPath: "#", Variant: ui.StatusInfo},
				{Label: "Mine", DismissPath: "#", Variant: ui.StatusNeutral},
			},
			ClearAllPath: "#",
		})
	}},
	{"inputgroup", "InputGroup", "Forms", "Input plus leading/trailing addon.", func() render.HTML {
		input := render.Tag("input", map[string]string{
			"type": "text", "name": "amount", "id": "demo-amount", "placeholder": "0.00",
		})
		return ui.InputGroup(ui.InputGroupConfig{
			Prepend: render.Text("$"),
			Input:   input,
			Append:  render.Text("USD"),
		})
	}},
	{"validationsummary", "ValidationSummary", "Forms", "Form-top error roll-up.", func() render.HTML {
		return ui.ValidationSummary(ui.ValidationSummaryConfig{
			Errors: ui.FieldErrors{
				"email":    "must be a valid email address",
				"password": "must be at least 8 characters",
			},
			FieldLabels: map[string]string{"email": "Email", "password": "Password"},
			FieldOrder:  []string{"email", "password"},
		})
	}},
	{"conditionalfield", "ConditionalField", "Forms", "Show/hide a form field based on a sibling value.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("ConditionalField is a runtime helper. Wire it inside a Form via field watchers."),
		)
	}},
	{"formrepeater", "FormRepeater", "Forms", "Add/remove rows of fields.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("FormRepeater renders a +/- chrome over a Repeater base. Per-page integration shown in the form demo."),
		)
	}},
	{"repeater", "Repeater", "Forms", "Generic repeatable group.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("Repeater is the headless variant of FormRepeater — bring your own chrome."),
		)
	}},

	// ---------- Data ----------
	{"datatable", "DataTable", "Data", "Sortable + paginated data table island.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("DataTable needs an RPC for sort/page/filter and a row data source. See /customers in examples/website for the full pattern."),
		)
	}},
	{"jsonviewer", "JSONViewer", "Data", "Pretty-printed expandable JSON.", func() render.HTML {
		return ui.JSONViewer(ui.JSONViewerConfig{
			Value: map[string]any{
				"id":     "01J7",
				"title":  "Hello",
				"status": "published",
				"tags":   []string{"a", "b"},
			},
			OpenDepth: 1,
		})
	}},
	{"diffviewer", "DiffViewer", "Data", "Unified or split diff display.", func() render.HTML {
		return ui.DiffViewer(ui.DiffViewerConfig{
			Patch: "--- old\n+++ new\n@@ -1,3 +1,4 @@\n line one\n-line two\n+line two MODIFIED\n line three\n+line four\n",
		})
	}},
	{"codeblock", "CodeBlock", "Data", "Plain code block (no highlight).", func() render.HTML {
		return ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `package main

func main() {
    println("hello")
}`})
	}},
	{"markdown", "Markdown", "Data", "Render Markdown source.", func() render.HTML {
		return ui.Markdown(ui.MarkdownConfig{Source: "# Hello\n\nThis is **bold**, this is *italic*, and this is `code`."})
	}},
	{"timeline", "Timeline", "Data", "Vertical event timeline.", func() render.HTML {
		return ui.Timeline(ui.TimelineConfig{
			Events: []ui.TimelineEvent{
				{Title: "Built", Meta: "Just now", Body: render.Text("go run . succeeded"), Variant: ui.TimelineSuccess},
				{Title: "Committed", Meta: "5m ago", Body: render.Text("feat: add timeline showcase"), Variant: ui.TimelineInfo},
				{Title: "Started", Meta: "1h ago", Body: render.Text("Working on components")},
			},
		})
	}},
	{"avatar", "Avatar", "Data", "User picture or initials.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.Avatar(ui.AvatarConfig{Name: "Donald Murillo"}),
			ui.Avatar(ui.AvatarConfig{Name: "Claude"}),
		)
	}},
	{"avatargroup", "AvatarGroup", "Data", "Stacked avatars with overflow chip.", func() render.HTML {
		return ui.AvatarGroup(ui.AvatarGroupConfig{
			Avatars: []ui.AvatarConfig{{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"}, {Name: "E"}, {Name: "F"}, {Name: "G"}},
			Max:     4,
		})
	}},
	{"statcard", "StatCard", "Data", "Metric tile with trend.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.StatCard(ui.StatCardConfig{Label: "Active users", Value: "12,483", Trend: "+8.2%", Direction: ui.TrendUp}),
			ui.StatCard(ui.StatCardConfig{Label: "Errors / hr", Value: "47", Trend: "−12%", Direction: ui.TrendDown}),
			ui.StatCard(ui.StatCardConfig{Label: "Latency p99", Value: "142ms", Trend: "stable", Direction: ui.TrendFlat}),
		)
	}},
	{"animatedcounter", "AnimatedCounter", "Data", "Number that animates on appearance.", func() render.HTML {
		return ui.AnimatedCounter(ui.AnimatedCounterConfig{To: 12483})
	}},
	{"rating", "Rating", "Data", "Star rating input or display.", func() render.HTML {
		return ui.RatingInput(ui.RatingConfig{Name: "rating", Label: "Rating", Max: 5, Value: 4})
	}},

	// ---------- Charts ----------
	{"barchart", "BarChart", "Charts", "Vertical bar chart.", func() render.HTML {
		return ui.BarChart(ui.BarChartConfig{
			Bars: []ui.BarChartBar{
				{Label: "Jan", Value: 12},
				{Label: "Feb", Value: 18},
				{Label: "Mar", Value: 9},
				{Label: "Apr", Value: 24},
				{Label: "May", Value: 19},
			},
			ShowAxis: true, ShowLabels: true,
		})
	}},
	{"linechart", "LineChart", "Charts", "Line / area chart.", func() render.HTML {
		return ui.LineChart(ui.LineChartConfig{
			Series: []ui.LineSeries{
				{Name: "Requests", Values: []float64{12, 18, 9, 24, 19, 22}},
			},
		})
	}},
	{"piechart", "PieChart", "Charts", "Pie / donut chart.", func() render.HTML {
		return ui.PieChart(ui.PieChartConfig{
			Slices: []ui.PieSlice{{Label: "Go", Value: 70}, {Label: "JS", Value: 18}, {Label: "SQL", Value: 12}},
		})
	}},
	{"sparkline", "Sparkline", "Charts", "Tiny inline trend line.", func() render.HTML {
		return ui.Sparkline(ui.SparklineConfig{Values: []float64{4, 6, 5, 8, 7, 10, 9, 12, 11, 14}})
	}},

	// ---------- Media ----------
	{"image", "OptimizedImage", "Media", "Image with width/height + lazy + srcset.", func() render.HTML {
		return ui.OptimizedImage(ui.OptimizedImageConfig{Src: "/__gofastr/app.css", Width: 320, Height: 180, Alt: "placeholder"})
	}},
	{"icon", "Icon", "Media", "Bundled SVG icon set with named lookup.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "demo-row"},
			ui.Icon("check", ui.IconConfig{AriaLabel: "Check"}),
			ui.Icon("x", ui.IconConfig{AriaLabel: "Close"}),
			ui.Icon("search", ui.IconConfig{AriaLabel: "Search"}),
		)
	}},
	{"gallery", "Gallery", "Media", "Image grid with lightbox.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("Gallery wraps OptimizedImage thumbnails + Lightbox. Live demo needs image sources."),
		)
	}},
	{"lightbox", "Lightbox", "Media", "Modal viewer for images.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("Lightbox is a widget you mount once + open from an island click."),
		)
	}},
	{"carousel", "Carousel", "Media", "Horizontal slider with snap.", func() render.HTML {
		return ui.Carousel(ui.CarouselConfig{
			Label: "Demo carousel",
			Slides: []ui.CarouselSlide{
				{Content: render.Tag("div", map[string]string{"class": "fact"}, render.Text("Slide 1"))},
				{Content: render.Tag("div", map[string]string{"class": "fact"}, render.Text("Slide 2"))},
				{Content: render.Tag("div", map[string]string{"class": "fact"}, render.Text("Slide 3"))},
			},
		})
	}},

	// ---------- Inputs (file / time / color) ----------
	{"fileupload", "FileUpload", "Inputs", "Single-file picker with preview.", func() render.HTML {
		return ui.FileUpload(ui.FileUploadConfig{Name: "avatar", Label: "Upload avatar", Accept: "image/*"})
	}},
	{"dropzone", "FileDropzone", "Inputs", "Drag-and-drop file upload.", func() render.HTML {
		return ui.FileDropzone(ui.FileDropzoneConfig{Name: "files", Label: "Drop files here", Multiple: true, MaxSizeMB: 10})
	}},
	{"timepicker", "TimePicker", "Inputs", "Hour + minute picker.", func() render.HTML {
		return ui.TimePicker(ui.TimePickerConfig{Name: "wakeup", Label: "Wake-up"})
	}},
	{"colorpicker", "ColorPicker", "Inputs", "Native swatch picker.", func() render.HTML {
		return ui.ColorPicker(ui.ColorPickerConfig{Name: "accent", Label: "Accent", Value: "#e0a040"})
	}},

	// ---------- Wizards + cross-cutting affordances ----------
	// StepWizard/ProgressSteps are Wizards; the rest below are
	// categorized into their real homes (Navigation/Feedback/Media)
	// via the Category field — they're grouped here only physically.
	{"stepwizard", "StepWizard", "Wizards", "Numbered multi-step form (server-driven).", func() render.HTML {
		return ui.StepWizard(ui.StepWizardConfig{
			Action:      "#",
			CurrentStep: 1,
			Steps: []ui.StepWizardStep{
				{Heading: "Account", Description: "Email + password"},
				{Heading: "Workspace", Description: "Pick a name"},
				{Heading: "Invite team", Description: "Optional"},
			},
		})
	}},
	{"progresssteps", "ProgressSteps", "Wizards", "Linear progress through ordered steps.", func() render.HTML {
		return ui.ProgressSteps(ui.ProgressStepsConfig{
			Steps: []ui.ProgressStep{
				{Label: "Plan", Status: ui.ProgressStepComplete},
				{Label: "Approve", Status: ui.ProgressStepCurrent},
				{Label: "Apply", Status: ui.ProgressStepUpcoming},
			},
		})
	}},
	{"optimisticaction", "OptimisticAction", "Feedback", "Action that commits + can rollback on error.", func() render.HTML {
		return ui.OptimisticAction(ui.OptimisticActionConfig{
			Endpoint:     "#",
			IdleLabel:    "Mark as read",
			SuccessLabel: "Marked ✓",
		})
	}},
	{"commandpalette", "CommandPalette", "Navigation", "⌘K modal palette — wired in nav (try it).", func() render.HTML {
		return html.Div(html.DivConfig{Class: "fact"},
			render.Text("CommandPalette returns a (trigger, *widget.Builder) pair — mount the modal once at app startup. Hit ⌘K (or click Search in the nav) to see the wired-up instance."),
		)
	}},
	{"globalsearch", "GlobalSearch", "Navigation", "Inline persistent search bar.", func() render.HTML {
		return html.Div(html.DivConfig{Class: "fact"},
			render.Text("GlobalSearch is the inline alternative to CommandPalette. Needs an RPC search endpoint."),
		)
	}},
	{"notificationbell", "NotificationBell", "Feedback", "Bell icon with unread badge + popover (trigger + widget).", func() render.HTML {
		// NotificationBell returns (trigger, *widget.Builder). The widget
		// must be mounted once at startup; the showcase renders only the
		// trigger HTML, paired with a static caption.
		trigger, _ := ui.NotificationBell(ui.NotificationBellConfig{
			Name:        "demo-bell",
			Label:       "Notifications",
			UnreadCount: 3,
			Items: []ui.NotificationItem{
				{Title: "Welcome to GoFastr", Time: "Just now"},
				{Title: "New release: v0.x.y", Time: "1h ago"},
			},
		})
		return html.Div(html.DivConfig{Class: "demo-stack"},
			trigger,
			html.Div(html.DivConfig{Class: "fact"},
				render.Text("NotificationBell returns a trigger HTML + a popover widget; mount the popover once at app startup via widget.Mount."),
			),
		)
	}},
	{"pipelineimage", "PipelineImage", "Media", "Image processed through the framework's image pipeline.", func() render.HTML {
		return render.Tag("div", map[string]string{"class": "fact"},
			render.Text("PipelineImage runs framework/image transforms (resize, webp) — see /examples for a live demo."),
		)
	}},
	// ---------- Interactivity ----------
	// Each pattern is its own page so the sidebar shows one entry per
	// behaviour. All share the "Interactivity" category.

	{"rpc-signal", "Click to Update", "Interactivity",
		"Click a button → server returns a value → it appears on screen without reloading.",
		func() render.HTML {
			// Live demo button: uses the interactive package.
			btn := interactive.OnClick(
				render.Tag("button", map[string]string{"class": "ui-button ui-button--primary"}, render.Text("Count")),
				interactive.Post("/__site/interactive/counter").
					OnSuccess(interactive.SetSignal("demo-counter")),
			)
			return html.Div(html.DivConfig{Class: "demo-stack"},
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("You have a counter, a vote button, or any UI where a click should update a number or string on screen — without a full page reload. The server owns the state; the browser just displays the latest value."),
				),
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("Put data-fui-rpc on a button and data-fui-rpc-signal on the same element. Add a data-fui-signal span wherever you want the response to appear. The runtime POSTs, parses JSON or text, and pushes the result into every matching signal node."),
				),
				ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `interactive.OnClick(
    render.Tag("button", nil, render.Text("Like")),
    interactive.Post("/api/like").
        OnSuccess(interactive.SetSignal("like-count")),
)`}),
				html.Div(html.DivConfig{Class: "demo-stage"},
					html.Div(html.DivConfig{Class: "demo-stage__label"}, render.Text("Live")),
					html.Div(html.DivConfig{Class: "demo-stage__viewport"},
						html.Div(html.DivConfig{Class: "demo-stack"},
							render.Tag("p", map[string]string{"class": "doc-head__lede"},
								render.Text("Click the button — the number updates from the server. No page reload."),
							),
							html.Div(html.DivConfig{Class: "demo-row"},
								btn,
								render.Tag("span", map[string]string{
									"data-fui-signal":          "demo-counter",
									"data-fui-signal-mode":     "text",
									"data-fui-flash-on-update": "",
									"class":                     "demo-signal-out",
								}, render.Text("0")),
							),
						),
					),
				),
			)
		}},

	{"rpc-open-widget", "Click to Open Popup", "Interactivity",
		"Click a button → server confirms → a modal pops up. No JavaScript needed.",
		func() render.HTML {
			btn := interactive.OnClick(
				render.Tag("button", map[string]string{"class": "ui-button ui-button--secondary"}, render.Text("Trigger Modal")),
				interactive.Post("/__site/interactive/open-drawer").
					OnSuccess(interactive.OpenWidget("demo-result-modal")),
			)
			return html.Div(html.DivConfig{Class: "demo-stack"},
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("A user submits a form or clicks an action, and on success a drawer or modal should appear — showing the result, a confirmation, or a next-step form. This is the \"do X, then show Y\" pattern."),
				),
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("Add data-fui-rpc-open=\"widget-name\" alongside data-fui-rpc. When the server returns 2xx, the runtime opens the named widget. The widget is pre-registered with widget.Mount at app startup; the RPC just triggers the reveal."),
				),
				ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `interactive.OnClick(
    render.Tag("button", nil, render.Text("Confirm")),
    interactive.Post("/api/action").
        OnSuccess(interactive.OpenWidget("result-modal")),
)`}),
				html.Div(html.DivConfig{Class: "demo-stage"},
					html.Div(html.DivConfig{Class: "demo-stage__label"}, render.Text("Live")),
					html.Div(html.DivConfig{Class: "demo-stage__viewport"},
						html.Div(html.DivConfig{Class: "demo-stack"},
							render.Tag("p", map[string]string{"class": "doc-head__lede"},
								render.Text("Click — a modal pops up after the POST succeeds."),
							),
							btn,
						),
					),
				),
			)
		}},

	{"rpc-form-signal", "Submit Without Reload", "Interactivity",
		"Submit a form and see the result inline — the page never reloads.",
		func() render.HTML {
			form := interactive.OnSubmit(
				render.Tag("form", map[string]string{"class": "demo-form-inline"},
					render.Tag("input", map[string]string{
						"type": "text", "name": "message", "placeholder": "Type something…",
						"required": "", "aria-label": "Message",
					}),
					render.Tag("button", map[string]string{
						"type":  "submit",
						"class": "ui-button ui-button--primary",
					}, render.Text("Send")),
				),
				interactive.Post("/__site/interactive/submit").
					OnSuccess(
						interactive.SetSignal("demo-form-result"),
						interactive.ResetForm(),
					),
			)
			return html.Div(html.DivConfig{Class: "demo-stack"},
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("A comment form, a search box, a quick-add field — submit without losing scroll position or context. The server processes it and returns a snippet (confirmation text, rendered item, status message) that appears right below the form."),
				),
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("Put data-fui-rpc on a <form> element. The runtime intercepts the submit, POSTs fields as JSON, and writes the response into the signal. Add data-fui-rpc-reset to clear the form after success so the user can submit again."),
				),
				ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `interactive.OnSubmit(
    render.Tag("form", nil,
        render.Tag("input", map[string]string{"name": "body", "required": ""}),
        render.Tag("button", map[string]string{"type": "submit"}, render.Text("Post")),
    ),
    interactive.Post("/api/comment").
        OnSuccess(
            interactive.SetSignal("comment-result"),
            interactive.ResetForm(),
        ),
)`}),
				html.Div(html.DivConfig{Class: "demo-stage"},
					html.Div(html.DivConfig{Class: "demo-stage__label"}, render.Text("Live")),
					html.Div(html.DivConfig{Class: "demo-stage__viewport"},
						html.Div(html.DivConfig{Class: "demo-stack"},
							render.Tag("p", map[string]string{"class": "doc-head__lede"},
								render.Text("Type a message and press Send. The response appears below; the form clears."),
							),
							form,
							render.Tag("div", map[string]string{
								"data-fui-signal":      "demo-form-result",
								"data-fui-signal-mode": "html",
								"class":                 "demo-signal-out",
							}),
						),
					),
				),
			)
		}},

	{"rpc-navigate", "Redirect After Action", "Interactivity",
		"Click a button → server confirms → you land on a new page, no full reload.",
		func() render.HTML {
			btn := interactive.OnClick(
				render.Tag("button", map[string]string{"class": "ui-button ui-button--ghost"}, render.Text("Navigate to Button →")),
				interactive.Post("/__site/interactive/navigate").
					OnSuccess(interactive.Navigate("/components/button")),
			)
			return html.Div(html.DivConfig{Class: "demo-stack"},
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("A user creates a resource (\"New project\") and on success should land on that resource's page. Or completes a wizard step and moves to the next. The server confirms the action, then the client transitions to the destination."),
				),
				render.Tag("p", map[string]string{"class": "doc-head__lede"},
					render.Text("Add data-fui-rpc-navigate=\"/path\" alongside data-fui-rpc. On 2xx the runtime calls history.pushState and fires the SPA router, swapping <main> content just like a link click — but only after the server confirms the action succeeded."),
				),
				ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: `interactive.OnClick(
    render.Tag("button", nil, render.Text("Create Project")),
    interactive.Post("/api/projects").
        OnSuccess(interactive.Navigate("/projects/new-id")),
)`}),
				html.Div(html.DivConfig{Class: "demo-stage"},
					html.Div(html.DivConfig{Class: "demo-stage__label"}, render.Text("Live")),
					html.Div(html.DivConfig{Class: "demo-stage__viewport"},
						html.Div(html.DivConfig{Class: "demo-stack"},
							render.Tag("p", map[string]string{"class": "doc-head__lede"},
								render.Text("Click — the page transitions to the Button component via SPA. Use the back button to return."),
							),
							btn,
						),
					),
				),
			)
		}},
}

// =============================================================================
// /components/  — the index page listing every catalog entry as a card,
// grouped by category. Re-uses .docs / .doc.. grid from the concepts page.
// =============================================================================

type ComponentsIndexScreen struct{}

func (s *ComponentsIndexScreen) ScreenTitle() string { return "Components" }
func (s *ComponentsIndexScreen) ScreenDescription() string {
	return "Every framework/ui and core-ui/patterns primitive, one page each."
}
func (s *ComponentsIndexScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ComponentsIndexScreen) Render() render.HTML {
	// The inner /components/* layout supplies the sidebar (ComponentsSidebar
	// component) — this screen is just the overview content cell. Grouped
	// card grid, no rail (the sidebar is the persistent nav).
	type group struct {
		Name    string
		Entries []componentEntry
	}
	var groups []group
	seen := map[string]int{}
	for _, c := range componentCatalog {
		if i, ok := seen[c.Category]; ok {
			groups[i].Entries = append(groups[i].Entries, c)
			continue
		}
		seen[c.Category] = len(groups)
		groups = append(groups, group{Name: c.Category, Entries: []componentEntry{c}})
	}

	hero := html.Div(html.DivConfig{Class: "components-overview__hero"},
		html.Div(html.DivConfig{Class: "mb-lg"}, tagAccent("Components · "+itoa(len(componentCatalog))+" primitives")),
		html.Heading(html.HeadingConfig{Level: 1, Class: "components-overview__title"},
			render.Text("Every primitive, "),
			html.Span(html.TextConfig{Class: "amber"}, render.Text("one page each")),
			render.Text("."),
		),
		render.Tag("p", map[string]string{"class": "components-overview__lede"},
			render.Text("framework/ui and core-ui/patterns ship typed Go constructors for each of the surfaces below. Pick from the sidebar — it stays put as you move between components."),
		),
	)

	sections := []render.HTML{}
	for _, g := range groups {
		cards := []render.HTML{}
		for _, c := range g.Entries {
			cards = append(cards, html.LinkHTML(html.LinkHTMLConfig{
				Href:  "/components/" + c.Slug,
				Class: "doc",
				Content: render.Join(
					html.Div(html.DivConfig{Class: "doc__head"},
						html.Span(html.TextConfig{Class: "pill ui"}, render.Text(g.Name)),
					),
					html.Div(html.DivConfig{Class: "doc__title"}, render.Text(c.Name)),
					html.Div(html.DivConfig{Class: "doc__desc"}, render.Text(c.Desc)),
					html.Div(html.DivConfig{Class: "doc__meta"}, render.Text("/components/"+c.Slug)),
				),
			}))
		}
		sections = append(sections, ui.Section(
			ui.SectionConfig{Heading: g.Name, Class: "intent"},
			html.Span(html.TextConfig{Class: "intent__meta"}, render.Text(itoa(len(g.Entries))+" primitives")),
			html.Div(html.DivConfig{Class: "docs"}, cards...),
		))
	}

	return render.Join(hero, html.Div(html.DivConfig{Class: "components-overview__sections"}, sections...))
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

// categorySlug — fragment-safe variant of a category name. "Buttons & links"
// → "buttons-links". Used as both the section <id> and the rail-link href so
// the anchor-scroll actually lands. Without this, hrefs end up like
// "#Buttons & links" which the browser silently ignores.
func categorySlug(name string) string {
	out := make([]byte, 0, len(name))
	prevDash := false
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
			prevDash = false
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
			prevDash = false
		default:
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	return string(out)
}

// =============================================================================
// /components/{slug} — single-component showcase page.
// =============================================================================

type ComponentShowcaseScreen struct {
	Entry componentEntry
}

func (s *ComponentShowcaseScreen) ScreenTitle() string {
	return s.Entry.Name
}
func (s *ComponentShowcaseScreen) ScreenDescription() string  { return s.Entry.Desc }
func (s *ComponentShowcaseScreen) ScreenType() app.ScreenType { return app.ScreenPage }

// demoStage renders the demo box with an honest label: "Live" for a
// real interactive instance, "Note" for a wiring explanation.
func (s *ComponentShowcaseScreen) demoStage() render.HTML {
	label := "Live"
	if noteOnlyComponents[s.Entry.Slug] {
		label = "Note"
	}
	return html.Div(html.DivConfig{Class: "demo-stage"},
		html.Div(html.DivConfig{Class: "demo-stage__label"}, render.Text(label)),
		html.Div(html.DivConfig{Class: "demo-stage__viewport"}, s.Entry.Demo()),
	)
}

func (s *ComponentShowcaseScreen) Render() render.HTML {
	return render.Tag("div", map[string]string{"class": "doc-shell-narrow"},
		render.Tag("article", map[string]string{"class": "doc-content"},
			render.Tag("nav", map[string]string{"class": "doc-crumbs", "aria-label": "Breadcrumb"},
				html.Link(html.LinkConfig{Href: "/components/", Text: "Components"}),
				html.Span(html.TextConfig{Class: "sep"}, render.Text("/")),
				html.Link(html.LinkConfig{Href: "/components/#" + categorySlug(s.Entry.Category), Text: s.Entry.Category}),
				html.Span(html.TextConfig{Class: "sep"}, render.Text("/")),
				html.Span(html.TextConfig{Class: "current"}, render.Text(s.Entry.Name)),
			),
			html.Div(html.DivConfig{Class: "doc-head"},
				html.Heading(html.HeadingConfig{Level: 1},
					render.Text(s.Entry.Name),
				),
				html.Div(html.DivConfig{Class: "doc-head__meta"},
					tagAccent(s.Entry.Category),
					// Real source package, linked to its API docs — this is
					// the per-component "usage/reference" the page otherwise
					// lacked. (Was hardcoded "framework/ui" for everything.)
					html.LinkHTML(html.LinkHTMLConfig{
						Href:       "https://pkg.go.dev/github.com/DonaldMurillo/gofastr/" + componentPkg(s.Entry.Slug),
						ExtraAttrs: html.Attrs{"rel": "external"},
						Content:    render.Join(render.Text(componentPkg(s.Entry.Slug)), render.Text(" ↗")),
					}),
				),
				render.Tag("p", map[string]string{"class": "doc-head__lede"}, render.Text(s.Entry.Desc)),
			),

			// Demo panel. Components that render a self-contained live
			// instance are labeled "Live"; ones that show an explanatory
			// note (need per-page wiring) are labeled "Note" so the box is
			// honest about what it's showing.
			s.demoStage(),
		),
	)
}
