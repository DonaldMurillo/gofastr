package ui

import (
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// VariantCSS is a registered variant's look as property, value pairs;
// "{colors.primary}" resolves to var(--color-primary).
type VariantCSS struct {
	// Props is the base state. Required.
	Props []string
	// Hover is emitted under :hover.
	Hover []string
	// Focus is emitted under :focus-visible.
	Focus []string
}

// StatusVariantCSS is a registered status variant's accent colour and icon.
type StatusVariantCSS struct {
	// Color is the accent. Required.
	Color string
	// Icon is Callout's and Notification's glyph, "•" when empty; no `"`, `\` or newline.
	Icon string
}

// RegisterButtonVariant registers a ButtonVariant for every button-classed
// component. Call it at init; it panics on an invalid, reserved or taken name,
// empty or odd Props, Hover or Focus, or after the ui-button sheet was built.
func RegisterButtonVariant(name string, css VariantCSS) ButtonVariant {
	buttonMods.register("RegisterButtonVariant", name, kindVariant, css)
	registerButtonClass(name)
	return ButtonVariant(name)
}

// RegisterButtonSize is RegisterButtonVariant for a ButtonSize.
func RegisterButtonSize(name string, css VariantCSS) ButtonSize {
	buttonMods.register("RegisterButtonSize", name, kindSize, css)
	registerButtonClass(name)
	return ButtonSize(name)
}

func registerButtonClass(name string) {
	buttonClasses[headless.Part("root--"+name)] = "fui-button--" + name
}

// RegisterCardVariant registers a CardVariant, with RegisterButtonVariant's panics.
func RegisterCardVariant(name string, css VariantCSS) CardVariant {
	cardMods.register("RegisterCardVariant", name, kindVariant, css)
	return CardVariant(name)
}

// RegisterStatusVariant registers a StatusVariant for StatusBadge, Tag, Callout
// and Notification. It panics like RegisterButtonVariant, and on an empty Color
// or an Icon holding `"`, `\` or a newline.
func RegisterStatusVariant(name string, css StatusVariantCSS) StatusVariant {
	if css.Color == "" {
		panic("ui: RegisterStatusVariant(" + name + "): Color is required")
	}
	if strings.ContainsAny(css.Icon, `"\`) || strings.ContainsAny(css.Icon, "\n\r") {
		panic("ui: RegisterStatusVariant(" + name + `): Icon must not contain '"', '\' or newlines`)
	}
	statusMods.register("RegisterStatusVariant", name, kindVariant, VariantCSS{})
	statusMods.mu.Lock()
	statusMods.status[name] = css
	statusMods.mu.Unlock()
	return StatusVariant(name)
}

type variantKind string

const (
	kindVariant variantKind = "Variant"
	kindSize    variantKind = "Size"
)

type variantSet struct {
	sheet    string
	reserved map[string]bool

	mu      sync.RWMutex
	sealed  bool
	kinds   map[string]variantKind
	entries map[string]VariantCSS
	status  map[string]StatusVariantCSS
}

var (
	buttonMods = &variantSet{
		sheet: "ui-button",
		reserved: map[string]bool{
			"primary": true, "secondary": true, "danger": true, "ghost": true,
			"small": true, "large": true,
		},
	}
	cardMods = &variantSet{
		sheet: "ui-card",
		reserved: map[string]bool{
			"outlined": true, "flat": true, "row": true, "interactive": true,
		},
	}
	statusMods = &variantSet{
		sheet: "ui-badge, ui-tag, ui-callout, ui-notification",
		reserved: map[string]bool{
			"success": true, "warning": true, "danger": true, "info": true,
			"neutral": true, "interactive": true, "floating": true,
			"at-top-right": true, "at-top-left": true,
			"at-bottom-right": true, "at-bottom-left": true,
		},
		status: map[string]StatusVariantCSS{},
	}
)

var buttonClasses = headless.Classes{
	headless.PartRoot: "fui-button",
	"root--primary":   "fui-button--primary",
	"root--secondary": "fui-button--secondary",
	"root--danger":    "fui-button--danger",
	"root--ghost":     "fui-button--ghost",
	"root--small":     "fui-button--small",
	"root--large":     "fui-button--large",
	"root--icon":      "fui-button--icon",
	headless.PartIcon: "fui-button__icon",
}

func buttonClassTokens(variant ButtonVariant, size ButtonSize) string {
	if variant == "" {
		variant = ButtonPrimary
	}
	classes := []string{buttonClasses.Class(headless.PartRoot)}
	if c := buttonClasses.Variant(headless.PartRoot, string(variant)); c != "" {
		classes = append(classes, c)
	}
	if c := buttonClasses.Variant(headless.PartRoot, string(size)); c != "" {
		classes = append(classes, c)
	}
	return strings.Join(classes, " ")
}

var fieldClasses = headless.Classes{
	headless.PartRoot:  "fui-field",
	headless.PartLabel: "fui-field__label",
	headless.PartHint:  "fui-field__hint",
	headless.PartError: "fui-field__error",
}

var formClasses = headless.Classes{
	headless.PartRoot:        "fui-form",
	headless.PartFormBody:    "fui-form__body",
	headless.PartFormActions: "fui-form__actions",
}

var inputClasses = headless.Classes{
	headless.PartRoot: "fui-input",
}

var selectClasses = headless.Classes{
	headless.PartRoot:   "fui-select",
	headless.PartOption: "fui-select__option",
}

var inputGroupClasses = headless.Classes{
	headless.PartRoot: "fui-input-group",
}

var validationSummaryClasses = headless.Classes{
	headless.PartRoot:      "fui-validation-summary",
	headless.PartTitle:     "fui-validation-summary__title",
	headless.PartErrorList: "fui-validation-summary__list",
	headless.PartErrorItem: "fui-validation-summary__item",
	headless.PartErrorLink: "fui-validation-summary__link",
}

var choiceClasses = headless.Classes{
	headless.PartRoot:    "fui-choice",
	headless.PartControl: "fui-choice__input",
	headless.PartText:    "fui-choice__text",
	headless.PartHint:    "fui-choice__hint",
}

var switchClasses = headless.Classes{
	headless.PartRoot:    "fui-switch",
	headless.PartControl: "fui-switch__input",
	headless.PartText:    "fui-switch__text",
}

var choiceGroupClasses = headless.Classes{
	headless.PartRoot:  "fui-choice-group",
	headless.PartLabel: "fui-choice-group__legend",
}

var passwordClasses = headless.Classes{
	headless.PartRoot:        "fui-password",
	headless.PartControl:     "fui-password__input",
	headless.PartAffixButton: "fui-password__reveal",
}

var colorClasses = headless.Classes{
	headless.PartRoot:        "fui-color",
	headless.PartControl:     "fui-color__text",
	headless.PartAffixSwatch: "fui-color__swatch",
}

var uploadClasses = headless.Classes{
	headless.PartRoot:      "fui-upload",
	headless.PartDropZone:  "fui-upload__zone",
	headless.PartText:      "fui-upload__label",
	headless.PartDropCTA:   "fui-upload__cta",
	headless.PartDropHint:  "fui-upload__hint",
	headless.PartDropInput: "fui-upload__input",
	headless.PartDropList:  "fui-upload__list",
	headless.PartStatus:    "fui-upload__status",
}

var textAreaClasses = headless.Classes{
	headless.PartRoot: "fui-textarea",
}

var whenClasses = headless.Classes{
	headless.PartRoot: "fui-when",
}

func (s *variantSet) register(api, name string, kind variantKind, css VariantCSS) {
	if !validVariantName(name) {
		panic("ui: " + api + "(" + name + "): name must be non-empty lowercase letters, digits, or hyphens")
	}
	if api != "RegisterStatusVariant" {
		if len(css.Props) == 0 {
			panic("ui: " + api + "(" + name + "): Props must be non-empty")
		}
		for label, pairs := range map[string][]string{
			"Props": css.Props, "Hover": css.Hover, "Focus": css.Focus,
		} {
			if len(pairs)%2 != 0 {
				panic("ui: " + api + "(" + name + "): " + label +
					" expects prop, value pairs; got an odd count")
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reserved[name] {
		panic("ui: " + api + "(" + name + "): name collides with a built-in " + s.sheet + " class")
	}
	if _, dup := s.kinds[name]; dup {
		panic("ui: " + api + "(" + name + "): duplicate registration, the name is already registered")
	}
	if s.sealed {
		panic("ui: " + api + "(" + name + ") called after the " + s.sheet +
			" stylesheet was built. Register custom variants at package init, before the app serves")
	}
	if s.kinds == nil {
		s.kinds = map[string]variantKind{}
		s.entries = map[string]VariantCSS{}
	}
	s.kinds[name] = kind
	s.entries[name] = css
}

func (s *variantSet) has(name string, kind variantKind) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.kinds[name] == kind
}

func (s *variantSet) sealAndSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sealed = true
	names := make([]string, 0, len(s.kinds))
	for n := range s.kinds {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func validVariantName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' {
			continue
		}
		return false
	}
	return name[0] != '-'
}

func checkButtonVariant(component string, v ButtonVariant) {
	switch v {
	case ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost:
		return
	}
	if buttonMods.has(string(v), kindVariant) {
		return
	}
	panic("ui: " + component + " unknown Variant " + string(v) +
		". Pick one of: primary, secondary, danger, ghost, or register it via ui.RegisterButtonVariant")
}

func checkButtonSize(component string, s ButtonSize) {
	switch s {
	case ButtonSizeDefault, ButtonSizeSmall, ButtonSizeLarge:
		return
	}
	if buttonMods.has(string(s), kindSize) {
		return
	}
	panic("ui: " + component + " unknown Size " + string(s) +
		". Pick one of: \"\" (default), small, large, or register it via ui.RegisterButtonSize")
}

func checkStatusVariant(component string, v StatusVariant) {
	switch v {
	case StatusSuccess, StatusWarning, StatusDanger, StatusInfo, StatusNeutral:
		return
	}
	if statusMods.has(string(v), kindVariant) {
		return
	}
	panic("ui: " + component + " unknown Variant " + string(v) +
		". Pick one of: success, warning, danger, info, neutral, or register it via ui.RegisterStatusVariant")
}

func checkCardVariant(v CardVariant) {
	switch v {
	case CardElevated, CardOutlined, CardFlat, CardRow:
		return
	}
	if cardMods.has(string(v), kindVariant) {
		return
	}
	panic("ui: Card unknown Variant " + string(v) +
		". Pick one of: \"\" (elevated), outlined, flat, row, or register it via ui.RegisterCardVariant")
}

func registeredStatusIcon(v StatusVariant) string {
	statusMods.mu.RLock()
	defer statusMods.mu.RUnlock()
	return statusMods.status[string(v)].Icon
}

func registeredStatusColor(name string) (color string, ok bool) {
	statusMods.mu.RLock()
	defer statusMods.mu.RUnlock()
	entry, hit := statusMods.status[name]
	if !hit {
		return "", false
	}
	return style.DefaultTheme().ResolveAll(entry.Color), true
}

func customModsCSS(set *variantSet, sheet, classPrefix string, t style.Theme) string {
	names := set.sealAndSnapshot()
	if len(names) == 0 {
		return ""
	}
	set.mu.RLock()
	entries := make(map[string]VariantCSS, len(names))
	for _, n := range names {
		entries[n] = set.entries[n]
	}
	set.mu.RUnlock()

	cs := style.NewComponentSheet(sheet, t)
	for _, n := range names {
		e := entries[n]
		cs.Rule("&." + classPrefix + "--" + n).Set(e.Props...)
		if len(e.Hover) > 0 {
			cs.Pseudo(":hover", e.Hover...)
		}
		if len(e.Focus) > 0 {
			cs.Pseudo(":focus-visible", e.Focus...)
		}
		cs.End()
	}
	return "\n" + cs.MustBuild()
}

func buttonModsCSS(t style.Theme) string {
	names := buttonMods.sealAndSnapshot()
	if len(names) == 0 {
		return ""
	}
	buttonMods.mu.RLock()
	entries := make(map[string]VariantCSS, len(names))
	for _, n := range names {
		entries[n] = buttonMods.entries[n]
	}
	buttonMods.mu.RUnlock()

	ss := style.NewStyleSheet(t)
	for _, n := range names {
		e := entries[n]
		ss.Rule(".fui-button--" + n).Set(e.Props...)
		if len(e.Hover) > 0 {
			ss.Pseudo(":hover", e.Hover...)
		}
		if len(e.Focus) > 0 {
			ss.Pseudo(":focus-visible", e.Focus...)
		}
		ss.End()
	}
	return "\n" + ss.CSS()
}

func customStatusCSS(sheet, classPrefix string, t style.Theme) string {
	names := statusMods.sealAndSnapshot()
	if len(names) == 0 {
		return ""
	}
	statusMods.mu.RLock()
	entries := make(map[string]StatusVariantCSS, len(names))
	for _, n := range names {
		entries[n] = statusMods.status[n]
	}
	statusMods.mu.RUnlock()

	cs := style.NewComponentSheet(sheet, t)
	for _, n := range names {
		e := entries[n]
		c := e.Color
		icon := e.Icon
		if icon == "" {
			icon = "•"
		}
		switch sheet {
		case "ui-badge", "ui-tag":
			cs.Rule("&."+classPrefix+"--"+n).Set(
				"background", statusTint(c, "15%", "85%"),
				"color", c,
				"border-color", statusTint(c, "30%", "70%"),
			).End()
		case "ui-callout":
			cs.Rule("&."+classPrefix+"--"+n).Set(
				"--ui-callout-accent", c,
				"--ui-callout-icon", `"`+icon+`"`,
			).End()
		case "ui-notification":
			cs.Rule("&."+classPrefix+"--"+n).
				Set("border-inline-start-color", c).
				Child("."+classPrefix+"__icon", "background", c).
				End()
		}
	}
	return "\n" + cs.MustBuild()
}

func statusTint(color, accentPct, surfacePct string) string {
	return "color-mix(in oklab, " + color + " " + accentPct +
		", var(--color-surface, #fff) " + surfacePct + ")"
}
