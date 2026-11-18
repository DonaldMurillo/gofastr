package ui

import (
	"context"
	"fmt"
	stdhtml "html"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// PageHeaderConfig configures a page-top header.
type PageHeaderConfig struct {
	Title    string      // required
	Subtitle string      // optional supporting text below the title
	Eyebrow  string      // optional small label above the title (e.g. "Customers")
	Actions  render.HTML // optional trailing action slot (button row, link)
	Badge    render.HTML // optional status beside the title; wraps below when needed
	// HeadingLevel overrides the title's heading level (default 1).
	HeadingLevel int
	Class        string
	ID           string
	// Compact removes the divider and outer padding for pane and article titles.
	Compact bool
	// ExtraAttrs reach the root <header>; class, id, style, role and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

var pageHeaderClasses = headless.Classes{
	headless.PartRoot:         "fui-page-header",
	headless.PartPageEyebrow:  "fui-page-header__eyebrow",
	headless.PartTitle:        "fui-page-header__title",
	headless.PartPageSubtitle: "fui-page-header__subtitle",
	headless.PartPageText:     "fui-page-header__text",
	headless.PartPageActions:  "fui-page-header__actions",
	headless.PartPageTitleRow: "fui-page-header__title-row",
}

// PageHeader renders a page-top header: title, qualifying text and the page's actions.
func PageHeader(cfg PageHeaderConfig) render.HTML {
	if cfg.Compact {
		cfg.Class += " fui-page-header--compact"
	}
	return pageHeaderStyle.WrapHTML(headless.PageHeader(headless.PageHeaderProps{
		Title:      cfg.Title,
		Level:      cfg.HeadingLevel,
		Eyebrow:    cfg.Eyebrow,
		Subtitle:   cfg.Subtitle,
		Actions:    cfg.Actions,
		Badge:      cfg.Badge,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role"),
		Parts:      rootClassParts(cfg.Class),
	}, pageHeaderClasses))
}

func slug(s string) string {
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
			prevDash = false
		case r > unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			out = append(out, unicode.ToLower(r))
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

// SectionConfig configures a content section. With no ID, the id is the
// Heading's slug; with neither Heading nor Label it renders a plain div.
type SectionConfig struct {
	Eyebrow     string // optional decorative kicker above the heading, aria-hidden
	Heading     string // optional <h2> heading
	Description string // optional supporting text under the heading
	// DescriptionHTML is the supporting text as markup; it wins over Description.
	DescriptionHTML render.HTML
	Label           string // accessible name when there is no Heading
	Class           string
	ID              string
	// Compact removes outer margins when a parent Stack owns section spacing.
	Compact bool
	// Overline draws the heading as a small muted group label over a run of cards.
	Overline bool
	// Band draws the section as a page band: display-size heading and more air.
	Band bool
	// ExtraAttrs reach the root; class, id, role, aria-label, aria-labelledby and data-cui-* are dropped.
	ExtraAttrs html.Attrs
	// Ctx scopes derived ids, so a repeated heading gets a "-2" suffix; an explicit ID is kept.
	Ctx context.Context
}

var sectionClasses = headless.Classes{
	headless.PartRoot:        "fui-section",
	headless.PartSectionBrow: "fui-section__eyebrow",
	headless.PartTitle:       "fui-section__heading",
	headless.PartDesc:        "fui-section__description",
	headless.PartHeader:      "fui-section__head",
	headless.PartSectionBody: "fui-section__body",
}

// Section renders a content section named by its heading or Label.
func Section(cfg SectionConfig, body ...render.HTML) render.HTML {
	if cfg.Compact {
		cfg.Class += " fui-section--compact"
	}
	if cfg.Overline {
		cfg.Class += " fui-section--overline"
	}
	if cfg.Band {
		cfg.Class += " fui-section--band"
	}
	sectionID := cfg.ID
	if sectionID != "" {
		render.ReserveID(cfg.Ctx, sectionID, headless.SectionIDChildren...)
	} else if cfg.Heading != "" {
		sectionID = render.UniqueID(cfg.Ctx, slug(cfg.Heading), headless.SectionIDChildren...)
	}
	return sectionStyle.WrapHTML(headless.Section(headless.SectionProps{
		Title:           cfg.Heading,
		Level:           2,
		Eyebrow:         cfg.Eyebrow,
		Description:     cfg.Description,
		DescriptionHTML: cfg.DescriptionHTML,
		Label:           cfg.Label,
		ID:              sectionID,
		ExtraAttrs:      headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label", "aria-labelledby"),
		Parts:           rootClassParts(cfg.Class),
		Ctx:             cfg.Ctx,
	}, sectionClasses, body...))
}

// FormFieldConfig configures a single form field row.
type FormFieldConfig struct {
	Label    string // required → <label>
	For      string // required → <label for=…> matches the control ID
	Help     string // optional helper text under the field
	Error    string // optional error message; non-empty marks the control invalid
	Required bool   // marks the label and the control
	// Input builds the control from the field's wiring (id, described-by,
	// invalid, required); use ui.Control, a typed field or headless.
	Input func(headless.FieldControl) render.HTML
	// ReserveError renders an empty, wired error node for a script to fill;
	// that script must also set aria-invalid.
	ReserveError bool
	Class        string
	// ExtraAttrs reach the root <div>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// FormField renders a labelled field; when both are set, Error renders before Help.
func FormField(cfg FormFieldConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: FormField requires Label")
	}
	if cfg.For == "" {
		panic("ui: FormField requires For (the control element's ID)")
	}
	if cfg.Input == nil {
		panic("ui: FormField requires Input — a builder that receives the field's wiring (headless.FieldControl); build the control with ui.Control, a typed field, or headless directly")
	}
	return formFieldStyle.WrapHTML(headless.Field(headless.FieldProps{
		Label: cfg.Label, For: cfg.For,
		Hint: cfg.Help, Error: cfg.Error, Required: cfg.Required,
		ReserveError: cfg.ReserveError,
		Parts:        rootClassParts(cfg.Class),
		ExtraAttrs:   html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, fieldClasses, cfg.Input))
}

// FormSectionConfig groups related fields under a heading + description.
type FormSectionConfig struct {
	Heading     string // optional
	Description string // optional
	Class       string
	// ID names the group and roots the description's id (<ID>-desc);
	// without it the id derives from Heading.
	ID string
	// ExtraAttrs reach the root <div> or <fieldset>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

var formSectionClasses = headless.Classes{
	headless.PartRoot:       "fui-form-section",
	headless.PartLegend:     "fui-form-section__heading",
	headless.PartGroupDesc:  "fui-form-section__description",
	headless.PartFields:     "fui-form-section__fields",
	headless.PartGroupError: "fui-form-section__error",
}

// FormSection groups FormFields: a fieldset and legend with a Heading, a div without.
func FormSection(cfg FormSectionConfig, fields ...render.HTML) render.HTML {
	return formSectionStyle.WrapHTML(headless.Fieldset(headless.FieldsetProps{
		Legend:      cfg.Heading,
		Description: cfg.Description,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:       rootClassParts(cfg.Class),
	}, formSectionClasses, fields...))
}

// ButtonVariant is a Button's semantic variant. Apps extend the set with
// RegisterButtonVariant; unregistered values panic at render.
type ButtonVariant string

const (
	ButtonPrimary   ButtonVariant = "primary"
	ButtonSecondary ButtonVariant = "secondary"
	ButtonDanger    ButtonVariant = "danger"
	ButtonGhost     ButtonVariant = "ghost"
)

// ButtonSize is the rendered size. The default keeps the touch-target
// floor; ButtonSizeSmall drops it for rows that provide the tap area.
type ButtonSize string

const (
	ButtonSizeDefault ButtonSize = ""
	ButtonSizeSmall   ButtonSize = "small"
	ButtonSizeLarge   ButtonSize = "large"
)

// ButtonConfig configures a button.
type ButtonConfig struct {
	Label string // required visible text and default accessible name
	// AriaLabel overrides the accessible name; an aria-label in ExtraAttrs is dropped.
	AriaLabel string
	Variant   ButtonVariant // defaults to ButtonPrimary
	Size      ButtonSize
	Type      string // "button" (default), "submit" or "reset"
	// Disabled renders the disabled state; a disabled key in ExtraAttrs panics.
	Disabled bool
	// ExtraAttrs reach the <button>. data-cui-* keys are runtime wiring
	// (interactive.Action.Attrs and friends); class, id, type, disabled and
	// aria-label are owned.
	ExtraAttrs html.Attrs
	ID         string
	Class      string
	Icon       string // a registered icon drawn before the label
	// IconOnly draws a registered Icon alone; Label becomes the accessible name.
	IconOnly bool
	// Shortcut is a chord ("Mod+S") that clicks the button; it needs ID.
	Shortcut string
	// QuietUntilDirty draws the secondary look until the button's form
	// holds unsaved edits; the form needs FormConfig.LeaveGuard.
	QuietUntilDirty bool
	// Ctx is the request context the Shortcut keycaps are said in.
	Ctx context.Context
}

// Button renders a semantic button. An unregistered Variant or Size panics.
func Button(cfg ButtonConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: Button requires Label")
	}
	v := cfg.Variant
	if v == "" {
		v = ButtonPrimary
	}
	checkButtonVariant("Button", v)
	checkButtonSize("Button", cfg.Size)
	action, extra := splitButtonAttrs(cfg.ExtraAttrs)
	aria := cfg.AriaLabel
	label, icon := buttonIcon("Button", cfg.Label, cfg.Icon, cfg.IconOnly, &aria)
	var suffix render.HTML
	if cfg.Shortcut != "" {
		if cfg.ID == "" {
			panic("ui: Button Shortcut needs ID — the chord clicks the button it names")
		}
		suffix = headless.Own(ShortcutHint(ShortcutHintConfig{Chord: cfg.Shortcut, BindTarget: "#" + cfg.ID, Ctx: cfg.Ctx}))
		if aria == "" {
			aria = cfg.Label
		}
		extra["aria-keyshortcuts"] = ariaKeyShortcuts(cfg.Shortcut)
	}
	cls := cfg.Class
	if cfg.QuietUntilDirty {
		cls = strings.TrimSpace("fui-button--until-dirty " + cls)
	}
	return buttonStyle.WrapHTML(headless.Button(headless.ButtonProps{
		Label:      label,
		AriaLabel:  aria,
		Icon:       icon,
		Suffix:     suffix,
		Variant:    string(v),
		Size:       string(cfg.Size),
		Type:       cfg.Type,
		Disabled:   cfg.Disabled,
		ID:         cfg.ID,
		Action:     action,
		ExtraAttrs: extra,
		Parts:      rootClassParts(cls),
	}, buttonClasses))
}

// LinkButtonConfig configures a button-styled <a> for navigation; use
// Button for in-page actions.
type LinkButtonConfig struct {
	Label   string        // required visible text
	Href    string        // required navigation target
	Variant ButtonVariant // defaults to ButtonPrimary
	Size    ButtonSize    // defaults to ButtonSizeDefault
	// External opens the link in a new tab with rel="noopener noreferrer".
	External bool
	Icon     string // a registered icon drawn before the label; unknown names draw none
	// IconOnly draws a registered Icon alone; Label becomes the accessible name.
	IconOnly bool
	ID       string
	Class    string
	// ExtraAttrs reach the <a>. Only the link wiring keys (push-state,
	// prefetch, open, deeplink, intercept-*, nav) pass among data-cui-*;
	// class, id and href are owned, and target and rel with External.
	ExtraAttrs html.Attrs
}

// LinkButton renders a button-styled anchor. It panics on an unsafe or
// protocol-relative Href.
func LinkButton(cfg LinkButtonConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: LinkButton requires Label")
	}
	if cfg.Href == "" {
		panic("ui: LinkButton requires Href. Use Button for non-navigating actions")
	}
	if isUnsafeScheme(cfg.Href) {
		panic("ui: LinkButton refuses unsafe Href scheme: " + cfg.Href)
	}
	if isCrossOriginAbsolute(cfg.Href) {
		panic("ui: LinkButton refuses a protocol-relative Href — name the scheme (https://…) or keep the path root-relative: " + cfg.Href)
	}
	v := cfg.Variant
	if v == "" {
		v = ButtonPrimary
	}
	checkButtonVariant("LinkButton", v)
	checkButtonSize("LinkButton", cfg.Size)
	action, extra := splitLinkAttrs(cfg.ExtraAttrs)
	aria := ""
	label, icon := buttonIcon("LinkButton", cfg.Label, cfg.Icon, cfg.IconOnly, &aria)
	return buttonStyle.WrapHTML(headless.Button(headless.ButtonProps{
		Label:      label,
		AriaLabel:  aria,
		Href:       cfg.Href,
		External:   cfg.External,
		Variant:    string(v),
		Size:       string(cfg.Size),
		ID:         cfg.ID,
		Icon:       icon,
		Action:     action,
		ExtraAttrs: extra,
		Parts:      rootClassParts(cfg.Class),
	}, buttonClasses))
}

func buttonIcon(comp, label, name string, only bool, aria *string) (string, render.HTML) {
	var icon render.HTML
	if name != "" && IconRegistered(name) {
		icon = Icon(name, IconConfig{Size: "18"})
	}
	if !only {
		return label, icon
	}
	if icon == "" {
		panic("ui: " + comp + " IconOnly needs a registered Icon, got " + strconv.Quote(name))
	}
	if *aria == "" {
		*aria = label
	}
	return "", icon
}

func rootClassParts(class string) headless.Parts {
	if class == "" {
		return headless.Parts{}
	}
	return headless.Parts{Attrs: headless.PartAttrs{
		headless.PartRoot: {"class": class},
	}}
}

func splitButtonAttrs(extra html.Attrs) (action, plain html.Attrs) {
	action, plain = html.Attrs{}, html.Attrs{}
	for k, v := range extra {
		lk := strings.ToLower(k)
		if _, dup := action[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		if _, dup := plain[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		switch {
		case lk == "disabled":
			panic("ui: Button ExtraAttrs carries disabled — use ButtonConfig.Disabled, the field owns the state")
		case lk == "aria-label":
		case strings.HasPrefix(lk, "data-cui-"), strings.HasPrefix(lk, "data-fui-"):
			action[lk] = v
		case strings.HasPrefix(lk, "data-hui-pane-"):
			action[lk] = v
		default:
			plain[lk] = v
		}
	}
	return action, plain
}

func splitLinkAttrs(extra html.Attrs) (action, plain html.Attrs) {
	action, plain = html.Attrs{}, html.Attrs{}
	for k, v := range extra {
		lk := strings.ToLower(k)
		if _, dup := action[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		if _, dup := plain[lk]; dup {
			panic("ui: ExtraAttrs carries " + lk + " under two spellings; one attribute, one spelling")
		}
		switch {
		case lk == "data-cui-push-state", lk == "data-cui-prefetch",
			lk == "data-cui-open", lk == "data-cui-deeplink",
			lk == "data-cui-intercept-page", lk == "data-cui-intercept-swap",
			lk == "data-cui-intercept-panel", lk == "data-cui-nav":
			action[lk] = v
		case strings.HasPrefix(lk, "data-cui-"), strings.HasPrefix(lk, "data-fui-"):
		default:
			plain[lk] = v
		}
	}
	return action, plain
}

func stripURLControls(href string) string {
	var b strings.Builder
	b.Grow(len(href))
	for i := range len(href) {
		c := href[i]
		if c == ' ' || c <= 0x1f || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isUnsafeScheme(href string) bool {
	s := stripURLControls(href)
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "javascript:") {
		return true
	}
	if strings.HasPrefix(lower, "vbscript:") {
		return true
	}
	if strings.HasPrefix(lower, "data:") {
		return true
	}
	return false
}

func isCrossOriginAbsolute(href string) bool {
	s := stripURLControls(href)
	return strings.HasPrefix(s, "//") || strings.HasPrefix(s, `/\`)
}

// StatusVariant is the tone of a StatusBadge, Callout, Tag, Notification
// or filter chip. Apps extend it with RegisterStatusVariant; unregistered
// values panic at render.
type StatusVariant string

const (
	StatusSuccess StatusVariant = "success"
	StatusWarning StatusVariant = "warning"
	StatusDanger  StatusVariant = "danger"
	StatusInfo    StatusVariant = "info"
	StatusNeutral StatusVariant = "neutral"
)

// StatusBadgeConfig configures a small status pill.
type StatusBadgeConfig struct {
	Label   string        // required visible text
	Variant StatusVariant // defaults to Neutral
	Dot     bool          // a decorative tone circle before the label
	ID      string
	Class   string
	// ExtraAttrs reach the root <span>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// StatusBadge renders a small inline pill whose label carries the state.
func StatusBadge(cfg StatusBadgeConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: StatusBadge requires Label")
	}
	v := cfg.Variant
	if v == "" {
		v = StatusNeutral
	}
	checkStatusVariant("StatusBadge", v)
	cls := "fui-badge--" + string(v)
	if cfg.Dot {
		cls += " fui-badge--dot"
	}
	cls = joinNonEmpty(cls, cfg.Class)
	return statusBadgeStyle.WrapHTML(headless.Badge(headless.BadgeProps{
		Label:      cfg.Label,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts:      rootClassParts(cls),
	}, badgeClasses))
}

var badgeClasses = headless.Classes{
	headless.PartRoot: "fui-badge",
}

// EmptyStateConfig configures an empty-state surface.
type EmptyStateConfig struct {
	Title       string      // required
	Description string      // optional supporting text
	Action      render.HTML // optional CTA (e.g. a button or link)
	ID          string
	Class       string
	// Compact draws one quiet line, for an empty list inside another screen.
	Compact bool
	// HeadingLevel overrides the title's heading level (1-6, default 3).
	HeadingLevel int
	// ExtraAttrs reach the root; class, id, style, role, aria-label, aria-labelledby and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

var emptyStateClasses = headless.Classes{
	headless.PartRoot:       "fui-empty-state",
	headless.PartEmptyTitle: "fui-empty-state__title",
	headless.PartEmptyDesc:  "fui-empty-state__description",
	headless.PartEmptyAct:   "fui-empty-state__action",
}

// EmptyState renders a nothing-here region named by its own heading.
func EmptyState(cfg EmptyStateConfig) render.HTML {
	if cfg.Compact {
		cfg.Class = cls("fui-empty-state--compact", cfg.Class)
	}
	return emptyStateStyle.WrapHTML(headless.EmptyState(headless.EmptyStateProps{
		Title:       cfg.Title,
		Level:       cfg.HeadingLevel,
		Description: cfg.Description,
		Action:      cfg.Action,
		ID:          cfg.ID,
		ExtraAttrs:  headless.Safe(cfg.ExtraAttrs, "class", "id", "role", "aria-label", "aria-labelledby"),
		Parts:       rootClassParts(cfg.Class),
	}, emptyStateClasses))
}

// CalloutConfig configures a persistent informational block.
type CalloutConfig struct {
	Title   string
	Variant StatusVariant // info | success | warning | danger | neutral
	ID      string
	Class   string
	// Standing keeps a danger or warning callout's tone without role=alert,
	// for a notice that is part of the page rather than news.
	Standing bool
	// ExtraAttrs reach the root; class, id, role and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

var calloutClasses = headless.Classes{
	headless.PartRoot:   "fui-callout",
	headless.PartHeader: "fui-callout__head",
	headless.PartTitle:  "fui-callout__title",
	headless.PartDesc:   "fui-callout__desc",
	headless.PartBody:   "fui-callout__body",
	headless.PartFooter: "fui-callout__footer",
}

// Callout renders a persistent inline message. Danger and warning
// callouts carry role=alert unless Standing.
func Callout(cfg CalloutConfig, body ...render.HTML) render.HTML {
	v := cfg.Variant
	if v == "" {
		v = StatusInfo
	}
	checkStatusVariant("Callout", v)
	live := headless.LiveOff
	if (v == StatusDanger || v == StatusWarning) && !cfg.Standing {
		live = headless.LiveAssertive
	}
	var bodyHTML render.HTML
	if len(body) > 0 {
		bodyHTML = render.Join(body...)
	}
	cls := joinNonEmpty("fui-callout--"+string(v), cfg.Class)
	return calloutStyle.WrapHTML(headless.Alert(headless.AlertProps{
		Title:      cfg.Title,
		Tone:       string(v),
		Body:       bodyHTML,
		Live:       live,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role"),
		Parts:      rootClassParts(cls),
	}, calloutClasses))
}

// TrendDirection indicates the direction of a stat trend.
type TrendDirection string

const (
	TrendUp   TrendDirection = "up"
	TrendDown TrendDirection = "down"
	TrendFlat TrendDirection = "flat"
)

// StatCardConfig configures a metric card.
type StatCardConfig struct {
	Label string // required (e.g. "Active users")
	Value string // required (e.g. "12,483" or "98.4%")
	Trend string // optional trend label (e.g. "+12% vs. last week")

	Direction TrendDirection // colors the trend pill; defaults to flat
	Href      string         // links the label to what the number counts
	Icon      string         // a registered icon in the card's head; unknown names draw none
	Action    render.HTML    // a control in the head, outside the label's link
	// Plain draws no frame, for a figure inside a StatStrip.
	Plain bool
	// Tile draws the Icon in a tinted tile beside the Action, the label below.
	Tile  bool
	ID    string
	Class string
	// ExtraAttrs reach the root <div>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

var statCardClasses = headless.Classes{
	headless.PartRoot:      "fui-stat-card",
	headless.PartHeader:    "fui-stat-card__head",
	headless.PartIcon:      "fui-stat-card__icon",
	headless.PartLabel:     "fui-stat-card__label",
	headless.PartStatValue: "fui-stat-card__value",
	headless.PartStatTrend: "fui-stat-card__trend",
	headless.PartStatLink:  "fui-stat-card__link",

	headless.Part("stat-trend--up"):   "fui-stat-card__trend--up",
	headless.Part("stat-trend--down"): "fui-stat-card__trend--down",
	headless.Part("stat-trend--flat"): "fui-stat-card__trend--flat",
}

// StatCard renders a metric card: label, value and an optional trend.
func StatCard(cfg StatCardConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: StatCard requires Label")
	}
	if cfg.Value == "" {
		panic("ui: StatCard requires Value")
	}
	dir := cfg.Direction
	if dir == "" {
		dir = TrendFlat
	}
	var icon render.HTML
	if cfg.Icon != "" && IconRegistered(cfg.Icon) {
		icon = Icon(cfg.Icon, IconConfig{})
	}
	return statCardStyle.WrapHTML(headless.StatCard(headless.StatCardProps{
		Label:      cfg.Label,
		Value:      cfg.Value,
		Trend:      cfg.Trend,
		Direction:  string(dir),
		Href:       cfg.Href,
		Icon:       icon,
		Action:     cfg.Action,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id"),
		Parts: rootClassParts(strings.TrimSpace(modifierClass("fui-stat-card--plain", cfg.Plain) + " " +
			modifierClass("fui-stat-card--tile", cfg.Tile) + " " + cfg.Class)),
	}, statCardClasses))
}

// AvatarSize is one of the predefined avatar sizes.
type AvatarSize string

const (
	AvatarSm AvatarSize = "sm" // ~1.5rem
	AvatarMd AvatarSize = ""   // default ~2.5rem
	AvatarLg AvatarSize = "lg" // ~3rem
	AvatarXl AvatarSize = "xl" // ~4rem
)

// AvatarStatus is a presence dot in the avatar's corner; the app supplies
// the status. Empty renders no dot.
type AvatarStatus string

const (
	AvatarStatusNone AvatarStatus = ""        // no dot (default)
	AvatarOnline     AvatarStatus = "online"  // success token
	AvatarAway       AvatarStatus = "away"    // warning token
	AvatarBusy       AvatarStatus = "busy"    // danger token
	AvatarOffline    AvatarStatus = "offline" // muted token
)

// AvatarConfig configures an avatar.
type AvatarConfig struct {
	Name   string     // required: alt text, and the initials when Src is empty
	Src    string     // optional image URL
	Size   AvatarSize // sm | "" (default md) | lg | xl
	Square bool       // a rounded square, for a logo or workspace tile
	Status AvatarStatus
	// StatusLabel overrides the dot's accessible name, by default the
	// status word in Ctx's language.
	StatusLabel string
	// Ctx is the request context the dot's default name is said in.
	Ctx   context.Context
	ID    string
	Class string
	// ExtraAttrs reach the root <span>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// Avatar renders an avatar image, or the name's initials without Src.
func Avatar(cfg AvatarConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: Avatar requires Name")
	}
	cls := "fui-avatar"
	if cfg.Size != AvatarMd {
		cls += " fui-avatar--" + string(cfg.Size)
	}
	if cfg.Square {
		cls += " fui-avatar--square"
	}
	if cfg.Status != AvatarStatusNone {
		cls += " fui-avatar--has-status"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	spanCfg := html.TextConfig{Class: cls, ID: cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)}

	var inner []render.HTML
	if cfg.Src != "" {
		px := strconv.Itoa(avatarPixels(cfg.Size))
		inner = append(inner, html.Image(html.ImageConfig{
			Src: cfg.Src, Alt: cfg.Name, Class: "fui-avatar__img",
			ExtraAttrs: html.Attrs{
				"width": px, "height": px,
				"loading": "lazy", "decoding": "async",
				"data-cui-internal": "",
			},
		}))
	} else {
		inner = append(inner,
			html.Span(html.TextConfig{
				Class:      "fui-avatar__initials",
				ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
			}, render.Text(initials(cfg.Name))),
			html.Span(html.TextConfig{Class: "fui-visually-hidden", ExtraAttrs: html.Attrs{"data-cui-internal": ""}},
				render.Text(cfg.Name)),
		)
	}
	if cfg.Status != AvatarStatusNone {
		inner = append(inner, avatarStatusDot(cfg.Ctx, cfg.Status, cfg.StatusLabel))
	}
	return avatarStyle.WrapHTML(html.Span(spanCfg, inner...))
}

// avatarPixels is the size's CSS default at a 16px root, so the image
// reserves a square box before it loads; the stylesheet sets the real size.
func avatarPixels(size AvatarSize) int {
	switch size {
	case AvatarSm:
		return 24
	case AvatarLg:
		return 48
	case AvatarXl:
		return 64
	default:
		return 40
	}
}

func avatarStatusDot(ctx context.Context, status AvatarStatus, label string) render.HTML {
	if label == "" {
		label = avatarStatusLabel(ctx, status)
	}
	return html.Span(html.TextConfig{
		Class: "fui-avatar__status fui-avatar__status--" + string(status),
		ExtraAttrs: html.Attrs{
			"role":              "img",
			"aria-label":        label,
			"data-cui-internal": "",
		},
	})
}

func avatarStatusLabel(ctx context.Context, status AvatarStatus) string {
	switch status {
	case AvatarOnline:
		return i18nui.AvatarStatusOnline.Text(ctx)
	case AvatarAway:
		return i18nui.AvatarStatusAway.Text(ctx)
	case AvatarBusy:
		return i18nui.AvatarStatusBusy.Text(ctx)
	case AvatarOffline:
		return i18nui.AvatarStatusOffline.Text(ctx)
	}
	return string(status)
}

func initials(name string) string {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		s := []rune(parts[0])
		if len(s) == 0 {
			return ""
		}
		return strings.ToUpper(string(s[0]))
	}
	first := []rune(parts[0])
	last := []rune(parts[len(parts)-1])
	out := []rune{}
	if len(first) > 0 {
		out = append(out, first[0])
	}
	if len(last) > 0 {
		out = append(out, last[0])
	}
	return strings.ToUpper(string(out))
}

// CodeBlockConfig configures a styled code-sample block.
type CodeBlockConfig struct {
	Code     string // raw source to render; escaped. Ignored when Lines is set.
	Language string // optional, used for aria-label only
	// Lines are trusted, pre-rendered source lines (e.g. highlighted); they win over Code.
	Lines    []render.HTML
	Filename string // renders a header with the filename and frames the block
	Label    string // the accessible name in place of "source code"
	ShowCopy bool   // a copy button in the header that copies the body
	// LineNumbers renders a gutter numbering each line.
	LineNumbers bool
	Scroll      bool // caps the body height and scrolls it; frames the block
	// HighlightLines bands the given 1-based lines.
	HighlightLines []LineRange
	// Diff marks lines starting with '+' as added and '-' as removed.
	Diff bool
	// HighlightWords marks literal matches inside each line's text.
	HighlightWords []string
	Wrap           bool // soft-wraps long lines instead of scrolling
	ID             string
	Class          string
	// ExtraAttrs reach the root; class, id, tabindex, aria-label and data-cui-* are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the request context the block's name, line count and copy control are said in.
	Ctx context.Context
}

// LineRange is a 1-based inclusive range of lines; To == 0 means From alone.
type LineRange struct {
	From int
	To   int
}

// ParseLineRanges parses "2" or "1,3-5"; an empty spec is no ranges, and
// anything but positive numbers and ascending ranges is an error.
func ParseLineRanges(spec string) ([]LineRange, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	parts := strings.Split(spec, ",")
	ranges := make([]LineRange, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		lo, hi, isRange := strings.Cut(p, "-")
		from, err := parseLineNumber(lo)
		if err != nil {
			return nil, err
		}
		r := LineRange{From: from}
		if isRange {
			r.To, err = parseLineNumber(hi)
			if err != nil {
				return nil, err
			}
			if r.To < r.From {
				return nil, fmt.Errorf("ui: line range %q ends before it starts", p)
			}
		}
		ranges = append(ranges, r)
	}
	return ranges, nil
}

func parseLineNumber(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("ui: %q is not a positive line number", s)
	}
	return n, nil
}

var codeBlockSeq atomic.Uint64

// CodeBlock renders a code sample: a bare scrollable <pre> for Code alone,
// a framed block with a header once Filename, ShowCopy, LineNumbers, Scroll
// or Lines is set.
func CodeBlock(cfg CodeBlockConfig) render.HTML {
	framed := cfg.Filename != "" || cfg.ShowCopy || cfg.LineNumbers || cfg.Scroll || len(cfg.Lines) > 0
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := i18nui.CodeBlockSource.Text(ctx)
	if cfg.Language != "" {
		label = i18nui.CodeBlockLangSource.Text(ctx, map[string]any{"language": cfg.Language})
	}
	if cfg.Label != "" {
		label = cfg.Label
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs, "tabindex", "aria-label")

	bodyID := cfg.ID
	if framed && bodyID != "" {
		bodyID += "-code"
	}
	if framed && cfg.ShowCopy && bodyID == "" {
		bodyID = "ui-code-block-" + strconv.FormatUint(codeBlockSeq.Add(1), 10)
	}
	perLine := len(cfg.HighlightLines) > 0 || cfg.Diff || len(cfg.HighlightWords) > 0
	var body render.HTML
	switch {
	case len(cfg.Lines) > 0:
		wrapped := make([]render.HTML, len(cfg.Lines))
		for i, ln := range cfg.Lines {
			content := string(ln)
			if len(cfg.HighlightWords) > 0 {
				content = markWords(content, cfg.HighlightWords)
			}
			wrapped[i] = codeBlockLine(cfg, i, len(cfg.Lines), content)
		}
		body = render.Join(wrapped...)
	case perLine:
		src := strings.Split(cfg.Code, "\n")
		if n := len(src); n > 1 && src[n-1] == "" {
			src = src[:n-1]
		}
		wrapped := make([]render.HTML, len(src))
		for i, raw := range src {
			content := render.Escape(raw)
			if len(cfg.HighlightWords) > 0 {
				content = markWords(content, cfg.HighlightWords)
			}
			wrapped[i] = codeBlockLine(cfg, i, len(src), content)
		}
		body = render.Join(wrapped...)
	default:
		body = render.Tag("code", nil, render.Text(cfg.Code))
	}

	if !framed {
		cls := "fui-code-block"
		if cfg.Wrap {
			cls += " fui-code-block--wrap"
		}
		if cfg.Class != "" {
			cls += " " + cfg.Class
		}
		preAttrs := extra
		if preAttrs == nil {
			preAttrs = html.Attrs{}
		}
		preAttrs["class"] = cls
		preAttrs["tabindex"] = "0"
		preAttrs["aria-label"] = label
		if bodyID != "" {
			preAttrs["id"] = bodyID
		}
		return codeBlockStyle.WrapHTML(render.Tag("pre", preAttrs, body))
	}

	cls := "fui-code-block fui-code-block--framed"
	if cfg.LineNumbers {
		cls += " fui-code-block--numbered"
	}
	if cfg.Scroll {
		cls += " fui-code-block--scroll"
	}
	if cfg.Wrap {
		cls += " fui-code-block--wrap"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	headChildren := []render.HTML{}
	if cfg.Filename != "" {
		headChildren = append(headChildren,
			html.Span(html.TextConfig{
				Class:      "fui-code-block__status",
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}),
			html.Span(html.TextConfig{Class: "fui-code-block__file"}, render.Text(cfg.Filename)),
		)
	}
	metaChildren := []render.HTML{}
	if len(cfg.Lines) > 0 {
		metaChildren = append(metaChildren,
			html.Span(html.TextConfig{}, render.Text(i18nui.CodeBlockLines.Text(ctx, map[string]any{"n": len(cfg.Lines)}))))
	}
	if cfg.ShowCopy {
		copyName := ""
		if cfg.Label != "" {
			copyName = i18nui.CodeBlockCopyOf.Text(ctx, map[string]any{"what": cfg.Label})
		}
		metaChildren = append(metaChildren, CopyButton(CopyButtonConfig{
			Target:       "#" + bodyID,
			Label:        i18nui.CodeBlockCopy.Text(ctx),
			CopiedLabel:  i18nui.CodeBlockCopied.Text(ctx),
			AnnounceText: i18nui.CopyCopied.Text(ctx),
			AriaLabel:    copyName,
			Ctx:          ctx,
			Class:        "fui-code-block__copy",
		}))
	}
	if len(metaChildren) > 0 {
		headChildren = append(headChildren,
			html.Div(html.DivConfig{Class: "fui-code-block__meta"}, metaChildren...))
	}
	head := html.Div(html.DivConfig{Class: "fui-code-block__head", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, headChildren...)

	preAttrs := map[string]string{"class": "fui-code-block__body", "tabindex": "0", "aria-label": label}
	if bodyID != "" {
		preAttrs["id"] = bodyID
	}
	if len(cfg.Lines) == 0 {
		preAttrs["data-cui-internal"] = ""
	}
	pre := render.Tag("pre", preAttrs, body)
	return codeBlockStyle.WrapHTML(
		html.Div(html.DivConfig{Class: cls, ID: cfg.ID, ExtraAttrs: extra}, head, pre))
}

func lineHighlighted(ranges []LineRange, n int) bool {
	for _, r := range ranges {
		if n < r.From {
			continue
		}
		if r.To == 0 {
			if n == r.From {
				return true
			}
			continue
		}
		if n <= r.To {
			return true
		}
	}
	return false
}

func firstTextByte(s string) byte {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				return 0
			}
			i += j
		case '&':
			return 0
		default:
			return s[i]
		}
	}
	return 0
}

func codeBlockLine(cfg CodeBlockConfig, i, n int, content string) render.HTML {
	class := codeBlockLineClass(cfg, i+1, content)
	if i < n-1 {
		content += "\n"
	}
	return html.Span(html.TextConfig{Class: class}, render.HTML(content))
}

func codeBlockLineClass(cfg CodeBlockConfig, n int, content string) string {
	cls := "fui-code-block__line"
	if lineHighlighted(cfg.HighlightLines, n) {
		cls += " fui-code-block__line--highlight"
	}
	if cfg.Diff {
		switch firstTextByte(content) {
		case '+':
			cls += " fui-code-block__line--added"
		case '-':
			cls += " fui-code-block__line--removed"
		}
	}
	return cls
}

func markWords(fragment string, words []string) string {
	if len(words) == 0 {
		return fragment
	}
	var b strings.Builder
	i := 0
	for i < len(fragment) {
		if fragment[i] == '<' {
			j := strings.IndexByte(fragment[i:], '>')
			if j < 0 {
				break
			}
			end := i + j + 1
			b.WriteString(fragment[i:end])
			i = end
			continue
		}
		end := len(fragment)
		if j := strings.IndexByte(fragment[i:], '<'); j >= 0 {
			end = i + j
		}
		node := fragment[i:end]
		if text := stdhtml.UnescapeString(node); containsAnyWord(text, words) {
			b.WriteString(markText(text, words))
		} else {
			b.WriteString(node)
		}
		i = end
	}
	b.WriteString(fragment[i:])
	return b.String()
}

func containsAnyWord(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func markText(text string, words []string) string {
	var b strings.Builder
	start := 0
	for i := 0; i < len(text); {
		if w := matchWordAt(text, i, words); w != "" {
			b.WriteString(render.Escape(text[start:i]))
			b.WriteString(`<mark class="fui-code-block__mark">`)
			b.WriteString(render.Escape(w))
			b.WriteString(`</mark>`)
			i += len(w)
			start = i
			continue
		}
		_, size := utf8.DecodeRuneInString(text[i:])
		if size == 0 {
			size = 1
		}
		i += size
	}
	b.WriteString(render.Escape(text[start:]))
	return b.String()
}

func matchWordAt(text string, at int, words []string) string {
	best := ""
	for _, w := range words {
		if len(w) > len(best) && at+len(w) <= len(text) && text[at:at+len(w)] == w {
			best = w
		}
	}
	return best
}

// SkipLinkConfig configures a skip-navigation link, shown on keyboard
// focus; place it first inside <body>.
type SkipLinkConfig struct {
	Target string // the id to jump to; defaults to "main-content"
	Text   string // the label; defaults to "Skip to main content" in Ctx's language
	Class  string
	ID     string
	// Ctx is the request context the default Text is said in.
	Ctx context.Context
	// ExtraAttrs reach the <a>; class, id, href and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// SkipLink renders a WCAG 2.4.1 skip-navigation link.
func SkipLink(cfg SkipLinkConfig) render.HTML {
	target := cfg.Target
	if target == "" {
		target = "main-content"
	}
	text := cfg.Text
	if text == "" {
		text = i18nui.SkipToMain.Text(cfg.Ctx)
	}
	cls := "fui-skip-link"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	return skipLinkStyle.WrapHTML(
		html.Link(html.LinkConfig{
			Href:       "#" + target,
			Text:       text,
			Class:      cls,
			ID:         cfg.ID,
			ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "href"),
		}),
	)
}
