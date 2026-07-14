// Package ui is the framework's opinionated component layer on top of
// core-ui.
//
// The split:
//
//	core-ui/        — unstyled building blocks (elements, widget, runtime)
//	framework/ui/   — semantic components that consume framework/ui/theme
//
// Components in this package express *product intent* — PageHeader,
// FormField, EmptyState, StatusBadge — rather than HTML primitives.
// Every visual decision routes through framework/ui/theme so a single
// token swap re-skins the whole app.
//
// Consumers import this package directly:
//
//	import "github.com/DonaldMurillo/gofastr/framework/ui"
//	import "github.com/DonaldMurillo/gofastr/framework/ui/theme"
//
//	page := ui.PageHeader(ui.PageHeaderConfig{
//	    Title:    "Customers",
//	    Subtitle: "1,283 active",
//	    Actions:  ui.Button(ui.ButtonConfig{Label: "Delete all", Variant: ui.ButtonDanger}),
//	})
//
// If a piece of work maps 1:1 to an HTML element or ARIA pattern, it
// belongs in core-ui. If it composes primitives to express intent, it
// belongs here.
//
// Component inventory (alphabetical; kept complete by
// TestDocGoInventoryComplete in inventory_test.go):
//
//	AnchoredRail         — sticky in-page nav rail with scrollspy wiring
//	AnimatedCounter      — scroll-triggered number tick animation
//	AspectRatioComponent — CLS-safe aspect-ratio wrapper (alias: AspectRatio)
//	AuthCard             — centered card shell for login/register/reset forms
//	Avatar               — circular image/initials avatar (sm/md/lg/xl)
//	AvatarGroup          — overlapping avatar stack with overflow chip
//	BackToTop            — fixed scroll-to-top affordance after a threshold
//	Banner               — page-level persistent status strip (dismissible)
//	BarChart             — categorical SVG bar chart
//	Box                  — padded/bordered layout box
//	Button               — primary/secondary/danger/ghost variants
//	Callout              — inline info/warning/danger/neutral block
//	Card                 — labelled <section> with header/body/footer
//	Carousel             — horizontal scroll-snap slider
//	Center               — layout centering wrapper
//	Checkbox             — labelled checkbox with FieldErrors wiring
//	CheckboxGroup        — <fieldset> of checkboxes with shared label + errors
//	Cluster              — horizontal layout that wraps by default (NoWrap opts out)
//	CodeBlock            — styled <pre><code> sample block
//	Collapsible          — styled <details> disclosure with summary
//	ColorPicker          — styled native <input type=color>
//	CommandPalette       — ⌘K modal + combobox composition
//	ConditionalField     — form section hidden until another field matches
//	ConditionalFieldVisible — inverse: visible until the field matches
//	ConfirmAction        — trigger + themed alertdialog modal pair
//	Container            — max-width page wrapper with breakpoint padding
//	CopyButton           — clipboard button with SR-announced confirmation
//	Counter              — signal-driven counter with +/− buttons
//	DataTable            — sortable/paginated table (island-friendly)
//	DetailList           — label/value description list for record detail
//	DiffViewer           — unified or split diff renderer
//	Divider              — <hr> for plain horizontal; role="separator" otherwise
//	DocLayout            — doc page skeleton (nav rail + article + pager)
//	EmptyState           — title/description/action block for no-data screens
//	FactBox              — labelled tile (label-first OR value-first KPI)
//	FileDropzone         — hero file-drop surface with image previews
//	FileUpload           — drag-drop file picker over <input type="file">
//	FilterChipBar        — role=toolbar of removable filter chips
//	FilterToolbar        — URL-driven filter/sort/search control strip
//	Form                 — opinionated <form> wrapper with submit + errors
//	FormField            — labelled input with required + help + error states
//	FormRepeater         — dynamic list of repeating field groups
//	FormSection          — grouped fields with heading + description
//	Gallery              — Grid/Strip/Masonry thumbnail surface
//	GlobalSearch         — inline persistent /-shortcut search bar
//	Grid                 — responsive auto-fit grid
//	Hero                 — centered landing hero
//	HeroSplit            — two-column hero (copy + media) with mobile collapse
//	InputGroup           — input with prepend/append addons
//	JSONViewer           — collapsible tree of arbitrary values
//	Lightbox             — zoom-overlay modal; pairs with Gallery
//	LineChart            — multi-series SVG time-series chart
//	Link                 — typed-variant anchor with unsafe-href sanitizing
//	LinkButton           — anchor styled as Button — for CTAs that navigate
//	Markdown             — themed wrapper over core/markdown
//	Menu                 — <details>-driven dropdown menu (keyboard + ARIA)
//	MetricBand           — compact semantic band of one to six related signals
//	Muted                — subdued inline <span> for secondary text
//	NetworkRetryBanner   — RPC-failure banner with health-probe retry
//	Notification         — toast-styled inline notification (variant + dismiss)
//	NotificationBell     — bell + unread badge + popover dropdown
//	NumberInput          — number field with explicit +/− step buttons
//	OptimisticAction     — instant-flip button with rollback on error
//	OptimizedImage       — responsive <picture> with srcset + lazy + Width/Height
//	PageHeader           — top-of-page header with title/eyebrow/subtitle/actions
//	PaneHost             — primary pane + openable secondary/tertiary side panes
//	PasswordInput        — password field with show/hide toggle
//	PieChart             — SVG ratio chart (donut variant via InnerRadius)
//	PipelineImage        — multi-format <picture> consuming framework/image
//	                       VariantSet output (typed sources + LQIP/BlurHash)
//	PollingIndicator     — pulsing dot confirming a polling RPC is firing
//	PricingCard          — plan tile with price + feature list + CTA
//	ProgressSteps        — linear step indicator (horizontal + vertical)
//	Radio                — labelled radio with FieldErrors wiring
//	RadioGroup           — <fieldset> of radios with shared label + errors
//	RangeSlider          — dual-thumb range with cross-clamp
//	RatingInput          — 1-N star/heart rating input
//	RecordSummary        — dominant record/event summary with bounded support rail
//	Repeater             — dynamic add/remove item list with min/max limits
//	Responsive           — viewport-swap pair (desktop / mobile variant)
//	SearchInput          — search field with icon prefix + clear button
//	Section              — labelled content section with heading + description
//	SegmentedControl     — radio-group styled as a sliding pill bar
//	Select               — labelled native <select> with help/error/placeholder
//	ShortcutHint         — OS-aware keyboard chord chips
//	Sidebar              — responsive primary navigation (inline/drawer)
//	SidebarBody          — nav content only, for a mirroring drawer slot
//	SignalToggle         — role=switch bound to a boolean signal
//	SignOut              — logout form POSTing the auth sign-out endpoint
//	SiteFooter           — multi-column footer grid + bottom strip
//	SiteHeader           — top bar with brand + nav + actions + mobile drawer
//	SkeletonAvatar       — circular shimmer placeholder
//	SkeletonCard         — card-shaped shimmer placeholder
//	SkeletonRow          — row-shaped shimmer placeholder
//	SkipLink             — focus-visible bypass link to main content
//	Slider               — <input type=range> with optional live value mirror
//	Sparkline            — pure-SVG inline trend chart
//	Spinner              — inline role="status" loading indicator
//	Stack                — vertical layout with gap
//	StatCard             — metric tile with label/value/trend
//	StatusBadge          — small status pill (success/warning/danger/info/neutral)
//	StatusPill           — compact status pill with optional leading dot
//	StepRail             — sticky numbered nav for multi-step pages
//	StepWizard           — multi-step form with a progress indicator bar
//	Sticky               — theme-token sticky wrapper (top/bottom pinning)
//	Switch               — iOS-style toggle (Checkbox variant)
//	TableOfContents      — auto-built sticky nav from <h2>/<h3>
//	Tabs                 — signal-driven tab strip
//	Tag                  — interactive pill (filter link or × dismiss)
//	TagInput             — free-form chips, Enter/comma to commit
//	TerminalBlock        — terminal transcript with a labelled header
//	TextArea             — multi-line input with typed Autogrow
//	Themed               — wraps a subtree in a registered theme override
//	ThemeToggle          — dark/light/auto toggle persisting color-scheme
//	Timeline             — vertical event rail
//	TimePicker           — styled native <input type=time>
//	ToggleAction         — three-state commit/untoggle button with mutex groups
//	Toolbar              — role=toolbar wrapper for grouped actions
//	Tooltip              — CSS-only hover/focus reveal
//	ValidationSummary    — inline summary of form validation errors
//
// Layout primitives (Stack, Cluster, Grid, Center, Spacer, Box) share
// one ui-layout stylesheet — see layout.go.
package ui
