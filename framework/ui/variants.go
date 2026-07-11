package ui

import (
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// ─── Custom variants ────────────────────────────────────────────────
//
// The built-in variant sets (ButtonPrimary…ButtonGhost, StatusSuccess…
// StatusNeutral, CardElevated…CardFlat) are validated at render time —
// an unknown value panics so typos surface immediately. Apps that need
// a brand variant register it here instead of shipping loose CSS: the
// registration extends the validation set AND routes the variant's CSS
// into the component's registered stylesheet, so it loads, scopes, and
// content-hashes exactly like the built-ins. Apps still ship zero
// bespoke CSS.
//
// Registration is init-time and process-global, matching
// registry.RegisterStyle semantics:
//
//	// package-level, in the app
//	var Brand = ui.RegisterButtonVariant("brand", ui.VariantCSS{
//	    Props: []string{"background", "{colors.primary}", "color", "#fff"},
//	})
//
//	// at render sites
//	ui.Button(ui.ButtonConfig{Label: "Buy", Variant: Brand})
//
// Registering after the component's stylesheet has been built (i.e.
// after the app started serving) panics — a late registration would
// pass validation but silently miss the sheet.

// VariantCSS declares the look of a registered custom variant as flat
// property/value pairs, the same shape style.StyleSheet.Set takes:
//
//	ui.VariantCSS{
//	    Props: []string{"background", "{colors.primary}", "color", "#fff"},
//	    Hover: []string{"filter", "none", "opacity", "0.9"},
//	}
//
// Values may reference theme tokens — "{colors.primary}" resolves to
// "var(--color-primary)" — so registered variants re-skin with the
// theme like every other component. The emitted rules are scoped to
// the component's data-fui-comp marker (e.g.
// `[data-fui-comp="ui-button"].ui-button--brand`), which outranks the
// base component rules, so Props override the default look without
// !important.
type VariantCSS struct {
	// Props is the variant's base-state declarations. Required.
	Props []string
	// Hover is emitted under :hover. Optional. Note the component base
	// sheet may apply its own hover treatment (Button dims via
	// `filter: brightness(0.95)`) — include "filter", "none" to replace
	// it rather than stack on it.
	Hover []string
	// Focus is emitted under :focus-visible. Optional; without it the
	// component's default focus ring applies.
	Focus []string
}

// StatusVariantCSS declares the palette of a registered custom status
// variant. Status variants are a color story: one accent color fans
// out to every status-coded component in that component's own pattern
// (badge/tag tint the pill from it, Callout and Notification color
// their accent rail and icon from it).
type StatusVariantCSS struct {
	// Color is the variant's accent. Required. Token references like
	// "{colors.primary}" resolve to "var(--color-primary)".
	Color string
	// Icon is the glyph Callout and Notification display for this
	// variant (a short string, typically one character). Defaults to
	// "•". Must not contain `"` or `\` — it is embedded in a CSS
	// string literal.
	Icon string
}

// RegisterButtonVariant registers a custom ButtonVariant under name and
// returns the typed value to pass as ButtonConfig.Variant /
// LinkButtonConfig.Variant (the two share the variant set and the
// ui-button stylesheet). The CSS lands in the registered ui-button
// sheet as `[data-fui-comp="ui-button"].ui-button--<name>` rules.
//
// Call at package init. Panics on: empty/invalid name (allowed:
// lowercase letters, digits, hyphens), a built-in or already-registered
// name, empty or odd-count Props/Hover/Focus, or registration after
// the ui-button sheet was built.
func RegisterButtonVariant(name string, css VariantCSS) ButtonVariant {
	buttonMods.register("RegisterButtonVariant", name, kindVariant, css)
	return ButtonVariant(name)
}

// RegisterButtonSize registers a custom ButtonSize under name (shared
// by Button and LinkButton, same rules as RegisterButtonVariant —
// sizes and variants share the ui-button--<name> class namespace, so
// a name can only be one or the other).
func RegisterButtonSize(name string, css VariantCSS) ButtonSize {
	buttonMods.register("RegisterButtonSize", name, kindSize, css)
	return ButtonSize(name)
}

// RegisterCardVariant registers a custom CardVariant under name. The
// CSS lands in the registered ui-card sheet as
// `[data-fui-comp="ui-card"].ui-card--<name>` rules. Same rules and
// panics as RegisterButtonVariant ("interactive" is reserved — Card
// uses it for the Href form).
func RegisterCardVariant(name string, css VariantCSS) CardVariant {
	cardMods.register("RegisterCardVariant", name, kindVariant, css)
	return CardVariant(name)
}

// RegisterStatusVariant registers a custom StatusVariant under name.
// One registration extends every StatusVariant consumer — StatusBadge,
// Tag (and therefore FilterChipBar chips), Callout, and Notification —
// each component deriving its own variant rules from the registered
// accent color in its own sheet, exactly as the built-ins do.
//
// Call at package init. Panics on: empty/invalid name, a built-in or
// already-registered name, an empty Color, an Icon containing `"` or
// `\`, or registration after any status-consuming sheet was built.
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

// ─── internals ──────────────────────────────────────────────────────

type variantKind string

const (
	kindVariant variantKind = "Variant"
	kindSize    variantKind = "Size"
)

// variantSet is the process-global record of registered custom
// variants for one component class namespace.
type variantSet struct {
	sheet    string          // component sheet the CSS routes into, e.g. "ui-button"
	reserved map[string]bool // built-in names + modifier classes sharing the namespace

	mu      sync.RWMutex
	sealed  bool
	kinds   map[string]variantKind
	entries map[string]VariantCSS
	status  map[string]StatusVariantCSS // statusMods only
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
			"outlined": true, "flat": true, "interactive": true,
		},
	}
	// statusMods guards one shared validation set for every
	// StatusVariant consumer. Reserved names cover the built-in
	// variants plus modifier classes any consumer already claims
	// (ui-tag--interactive, ui-notification--floating/--at-*).
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
		panic("ui: " + api + "(" + name + "): duplicate registration — the name is already registered")
	}
	if s.sealed {
		panic("ui: " + api + "(" + name + ") called after the " + s.sheet +
			" stylesheet was built — register custom variants at package init, before the app serves")
	}
	if s.kinds == nil {
		s.kinds = map[string]variantKind{}
		s.entries = map[string]VariantCSS{}
	}
	s.kinds[name] = kind
	s.entries[name] = css
}

// has reports whether name is registered with the given kind.
func (s *variantSet) has(name string, kind variantKind) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.kinds[name] == kind
}

// sealAndSnapshot marks the set sealed (no further registrations) and
// returns the registered names in stable order plus the entry maps.
// Called from the component styleFns, so the seal coincides with the
// first materialization of a consuming sheet.
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

// ─── render-time validation helpers ─────────────────────────────────

func checkButtonVariant(component string, v ButtonVariant) {
	switch v {
	case ButtonPrimary, ButtonSecondary, ButtonDanger, ButtonGhost:
		return
	}
	if buttonMods.has(string(v), kindVariant) {
		return
	}
	panic("ui: " + component + " unknown Variant " + string(v) +
		" — pick one of: primary, secondary, danger, ghost, or register it via ui.RegisterButtonVariant")
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
		" — pick one of: \"\" (default), small, large, or register it via ui.RegisterButtonSize")
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
		" — pick one of: success, warning, danger, info, neutral, or register it via ui.RegisterStatusVariant")
}

func checkCardVariant(v CardVariant) {
	switch v {
	case CardElevated, CardOutlined, CardFlat:
		return
	}
	if cardMods.has(string(v), kindVariant) {
		return
	}
	panic("ui: Card unknown Variant " + string(v) +
		" — pick one of: \"\" (elevated), outlined, flat, or register it via ui.RegisterCardVariant")
}

// registeredStatusIcon returns the registered icon glyph for a custom
// status variant ("" when the variant isn't registered).
func registeredStatusIcon(v StatusVariant) string {
	statusMods.mu.RLock()
	defer statusMods.mu.RUnlock()
	return statusMods.status[string(v)].Icon
}

// registeredStatusColor returns the resolved accent CSS color for a
// registered custom status variant, with its {colors.*} token shorthand
// expanded to var(--color-*) the same way customStatusCSS does (the
// resolution is purely syntactic — var names come from the token name,
// never the value — so DefaultTheme yields the same result as any host
// theme). ok is false when name is not a registered status variant.
func registeredStatusColor(name string) (color string, ok bool) {
	statusMods.mu.RLock()
	defer statusMods.mu.RUnlock()
	entry, hit := statusMods.status[name]
	if !hit {
		return "", false
	}
	return style.DefaultTheme().ResolveAll(entry.Color), true
}

// ─── CSS emission (called from the component styleFns) ──────────────

// customModsCSS renders the registered entries of set as scoped rules
// in the named component sheet, using classPrefix--<name> selectors on
// the marker element. Seals the set.
//
// extraScopes lists additional data-fui-comp markers whose elements
// carry the same modifier classes — components like ToggleAction
// render class="ui-button ui-button--<variant>" under their OWN
// marker (ui-toggle-action), so rules scoped only to ui-button would
// never match them. Each extra scope gets a full copy of the variant
// rules (the built-in variants don't need this: they're plain
// .ui-button--<name> class rules that match under any marker).
func customModsCSS(set *variantSet, sheet, classPrefix string, t style.Theme, extraScopes ...string) string {
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

	var out strings.Builder
	for _, scope := range append([]string{sheet}, extraScopes...) {
		cs := style.NewComponentSheet(scope, t)
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
		out.WriteString("\n")
		out.WriteString(cs.MustBuild())
	}
	return out.String()
}

// customStatusCSS renders the registered status variants into one
// consuming component's sheet, following that component's own built-in
// variant pattern. Seals the status set on the first consuming build.
func customStatusCSS(component string, t style.Theme) string {
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

	cs := style.NewComponentSheet(component, t)
	for _, n := range names {
		e := entries[n]
		c := e.Color
		icon := e.Icon
		if icon == "" {
			icon = "•"
		}
		switch component {
		case "ui-badge", "ui-tag":
			// Same soft-tint pattern as the built-in success/warning/…
			// rules: 15% accent surface, full-accent text, 30% border.
			cs.Rule("&."+component+"--"+n).Set(
				"background", statusTint(c, "15%", "85%"),
				"color", c,
				"border-color", statusTint(c, "30%", "70%"),
			).End()
		case "ui-callout":
			// Callout's variant hook is a pair of custom properties the
			// base rules consume.
			cs.Rule("&.ui-callout--"+n).Set(
				"--ui-callout-accent", c,
				"--ui-callout-icon", `"`+icon+`"`,
			).End()
		case "ui-notification":
			cs.Rule("&.ui-notification--"+n).
				Set("border-inline-start-color", c).
				Child(".ui-notification__icon", "background", c).
				End()
		}
	}
	return "\n" + cs.MustBuild()
}

// statusTint mixes the accent color with the surface token — the same
// color-mix recipe the built-in status variants use.
func statusTint(color, accentPct, surfacePct string) string {
	return "color-mix(in oklab, " + color + " " + accentPct +
		", var(--color-surface, #fff) " + surfacePct + ")"
}
