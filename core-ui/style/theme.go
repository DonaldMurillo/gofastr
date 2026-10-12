package style

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// Theme is the canonical typed design system.
//
// Every field is required: apps must define every primitive token.
// The framework ships a fully-populated DefaultTheme() so the
// scaffold can start from a working baseline; users edit values,
// not whether the field exists.
//
// Apps that need extra tokens beyond the canonical set embed Theme
// in their own struct:
//
//	type AppTheme struct {
//	    style.Theme
//	    Brand struct{ Logo, Glow style.Color }
//	}
//
// Framework code (framework/ui components, widget theming, the
// catalog endpoint) only sees the embedded canonical fields. App-
// specific components reference theme.App.Brand.Logo directly.
type Theme struct {
	Name string // theme identifier, used for telemetry and the class-scoped block name

	// DarkColors is the dark-scheme palette, keyed by color token name
	// ("background", "surface", "text", "primary", …). When non-empty,
	// CSSCustomProperties emits a `:root[data-color-scheme="dark"]` block (and a
	// matching prefers-color-scheme fallback) re-declaring these tokens, so a
	// ui.ThemeToggle / the color-scheme bootstrap recolors the whole app, and
	// every surface that emits the theme CSS, by flipping one attribute. Empty
	// by default: an app opts into dark mode by supplying it, so existing
	// light-only apps are never surprised into dark by an OS preference. It's a
	// map (not a typed ColorSet) so the reflection token-walk ignores it.
	DarkColors map[string]string

	// DarkCode is the dark-scheme syntax palette, keyed by code token
	// name ("kw", "str", …). Same contract as DarkColors, when
	// non-empty its `--tk-<name>` re-declarations join the dark-scheme
	// blocks, so a theme toggle restyles code blocks along with the
	// rest of the page. Empty by default. A map (not a typed CodeSet)
	// so the reflection token-walk ignores it.
	DarkCode map[string]string

	// Components is the canonical flattened component-option set, keyed
	// like "density" or "button.treatment" with one lowercase word as the
	// value. It is the theme's answer to questions a component family
	// asks about its own drawing (how dense, which treatment, which
	// radius), not a token: it never appears on the reflection token
	// walk and cannot be read as a --color/--spacing var. A map (not a
	// typed struct) so the walk ignores it, exactly like DarkColors.
	//
	// core-ui/style stores, validates, hashes and copies this map but
	// cannot turn it into CSS; the compiler that can is registered once
	// per process via RegisterComponentOptionsCompiler (framework/ui
	// registers it from its init), and its declarations join the :root
	// block and every theme-override scope block. Empty by default.
	Components map[string]string

	// Knobs sets the per-component --ui-* variables from the theme,
	// keyed without the leading dashes ("ui-sidebar-width": "16rem").
	// Every component dimension the scale tokens do not cover reads
	// one of these with its default as the var() fallback, so a theme
	// reaches every value in the kit from one place. A knob is
	// declared at :root and in every scope block the theme emits; a
	// value that reads a token (var(--color-border-strong)) resolves
	// against the palette where it is declared, so a ui.Themed scope
	// with its own palette sets the knob on its own theme too. Keys
	// must start with "ui-"; values pass the free-form token check.
	// Empty by default. A map (not a typed struct) so the reflection
	// token-walk ignores it, like DarkColors.
	Knobs map[string]string

	Colors      ColorSet
	Spacing     SpacingScale
	Radii       RadiusSet
	Strokes     StrokeSet
	Leading     LeadingSet
	Tracking    TrackingSet
	Opacities   OpacitySet
	Fonts       FontSet
	Breakpoints BreakpointSet
	Shadows     ShadowSet
	ZIndex      ZIndexSet
	Durations   DurationSet
	Easings     EasingSet
	Typography  FontSizeSet
	FontWeights FontWeightSet
	Layout      LayoutSet
	Code        CodeSet

	// Extensions holds the app's own token sets, added with Extend:
	// pointers to structs of typed tokens, each token emitting under its
	// type's prefix like a built-in (a style.Size field is a --size-*
	// variable). Set it through Extend, which copies its inputs, fills
	// in names and refuses a key another token already emits; Validate
	// refuses a duplicate key however the slice was built.
	Extensions []any
}

// ColorSet is the canonical palette. Every theme must declare every
// field; framework/ui components reference them by name.
type ColorSet struct {
	Primary, PrimaryFg               Color
	Secondary, SecondaryFg           Color
	Background, Surface, SurfaceSoft Color
	Text, TextMuted, TextSubtle      Color
	Border, BorderStrong             Color
	// Danger is the one status tone whose filled-control ink is its own
	// token (danger-fg) rather than primary-fg: a host whose primary is
	// light with dark ink (amber, pastel) would otherwise paint an
	// unreadable filled danger button. Validate refuses a pair below
	// 4.5:1 when both values are plain hex.
	Danger, DangerFg       Color
	Success, Warning, Info Color
	Accent                 Color

	// Code surface, the background + foreground used by code-display
	// components (ui.CodeBlock, demo source panels). Intentionally a
	// SEPARATE token pair from Surface/Text so dark mode can keep code
	// blocks legibly distinct from the page background without
	// inverting (light text on dark background works in both schemes).
	CodeSurface, CodeText, CodeBorder Color
}

// SpacingScale: pixel-valued spacing scale.
type SpacingScale struct {
	XS, SM, MD, LG, XL, XXL, XXXL Spacing
}

// RadiusSet: border-radius scale.
type RadiusSet struct {
	None, SM, MD, LG, XL, Full Radius
}

// StrokeSet: line widths. Thin draws a control's or card's border and
// every divider, Thick an emphasised border (a selected card, a tab's
// active rule), Focus the keyboard focus outline and FocusOffset its
// gap from the element.
type StrokeSet struct {
	Thin, Thick, Focus, FocusOffset Stroke
}

// LeadingSet: line heights. Tight sets headings and display type, Snug
// labels, captions and dense rows, Normal body copy and controls,
// Relaxed long-form reading text.
type LeadingSet struct {
	Tight, Snug, Normal, Relaxed LineHeight
}

// TrackingSet: letter spacing. The negative steps tighten headings and
// display type (Tighter the largest), Wide and Wider space out small
// upper-case labels.
type TrackingSet struct {
	Tighter, Tight, Snug, Wide, Wider LetterSpacing
}

// OpacitySet: the opacities a state or a decoration fades to. Disabled
// dims a control that cannot be used, Muted a secondary glyph or an
// inactive item, Faint a decorative fill such as a chart's area.
type OpacitySet struct {
	Faint, Disabled, Muted Opacity
}

// FontSet: font-family stacks.
type FontSet struct {
	Body, Heading, Mono Font
}

// BreakpointSet: viewport-width thresholds, in pixels.
type BreakpointSet struct {
	SM, MD, LG, XL, XXL Breakpoint
}

// ShadowSet: box-shadow depth scale. XS is the hairline lift a resting
// control (button, input, select) carries; SM sits under a card, MD
// under a popover or menu, LG under a dialog.
type ShadowSet struct {
	None, XS, SM, MD, LG, XL Shadow
}

// ZIndexSet: named layers. Prevents the `z-index: 9999` arms race,
// every elevated surface picks a layer by name.
type ZIndexSet struct {
	Dropdown, Sticky, Modal, Popover, Toast ZIndexValue
}

// DurationSet: animation / transition timing scale.
//
// The generic Fast/Normal/Slow are everyday tokens. The named overlay
// and toast/dropdown values are referenced by core-ui widget chrome so
// every modal, drawer, dropdown, and toast respects the same motion
// budget, and a single override on the theme retunes them all.
type DurationSet struct {
	Fast, Normal, Slow Duration

	// OverlayEnter is how long modal/drawer surfaces take to slide
	// or fade in. OverlayExit covers the matching close animation.
	OverlayEnter, OverlayExit Duration

	// ToastEnter / ToastExit cover slide-in and dismiss of toast items.
	ToastEnter, ToastExit Duration

	// DropdownEnter covers fade/scale-in for anchored dropdown menus.
	DropdownEnter Duration
}

// EasingSet: CSS timing-function tokens. Widget chrome references
// these so motion curves stay theme-driven.
type EasingSet struct {
	// EaseOut decelerates, the default for elements entering view.
	// EaseIn accelerates, used when elements leave view.
	// EaseInOut for symmetric transitions. Spring overshoots slightly.
	EaseOut, EaseIn, EaseInOut, Spring Easing
}

// FontSizeSet: typography size scale.
type FontSizeSet struct {
	XS, SM, Base, LG, XL, XXL, XXXL FontSize
}

// CodeSet: the syntax-highlight palette consumed by code-display
// components via `--tk-<name>`. Field names ARE the emitted suffixes
// (KW → --tk-kw): KW keywords, FN function names, Str strings, Num
// numeric literals, Com comments, Type type names, PN punctuation.
// The group is optional, zero tokens are skipped (component CSS
// keeps its built-in fallback palette), so themes that never set it
// behave exactly as before the group existed. Dark-scheme values go
// in Theme.DarkCode.
type CodeSet struct {
	KW, FN, Str, Num, Com, Type, PN CodeColor
}

// FontWeightSet: the weights a design uses, by role.
type FontWeightSet struct {
	Normal, Medium, Semibold, Bold FontWeight
}

// LayoutSet: the dimensions a page is built around.
//
// TouchTarget is the WCAG 2.5.5 minimum tap target (default 44px);
// pagination, inputs and the mobile hamburger summary reference
// var(--spacing-touch-target) directly, while comfortable-density
// controls reach it through the --fui-density-control-h option
// variable (which the framework/ui compiler draws from this token).
//
// PageWidth is the page column a site's header, main and footer share
// (ui.Container's page width); PageGutter is the side space outside it.
// HeaderHeight is the top bar's height, which a viewport-filling row
// subtracts. NarrowWidth, ContentWidth and WideWidth are ui.Container's
// narrow, default and wide caps.
type LayoutSet struct {
	TouchTarget Spacing

	PageWidth, PageGutter, HeaderHeight Size

	NarrowWidth, ContentWidth, WideWidth Size
}

// AutoFillNames walks every typed token field of t and, for any
// token whose Name is empty, assigns the canonical token name derived
// from the Go struct-field path. Authors can write
//
//	t.Colors.Primary = style.Color{Value: "#FF0000"}
//
// the Name "primary" is filled in automatically. Explicit Name
// values are preserved (handy for app extensions that need a
// non-canonical CSS var identifier).
//
// Called automatically by App.WithTheme before validation, so
// authors never have to invoke it directly.
func AutoFillNames(t *Theme) {
	autofillTokens(reflect.ValueOf(t).Elem(), nil)
}

// autofillTokens walks the struct, recursing into named sub-structs
// (Colors, Spacing, …). When it reaches a typed-token leaf (Color,
// Spacing, …) with an empty Name, it assigns the name derived from
// the most-recent struct field name visited.
//
// path[len-1] is the immediate field name (e.g. "Primary");
// derivedTokenName maps it to the canonical CSS variable suffix
// (kebab-case, with the size-scale steps XXL/XXXL spelled 2xl/3xl).
func autofillTokens(v reflect.Value, path []string) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		// Extensions: each entry is its own token set, named from its
		// own fields, so the slice's field name is not passed down.
		for i := range v.Len() {
			autofillTokens(v.Index(i), nil)
		}
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	// CodeColor is the one OPTIONAL token type: a fully-unset token
	// stays zero (skipped by validation + emission, component CSS
	// falls back), so only autofill the Name once a Value was set.
	if v.Type() == reflect.TypeFor[CodeColor]() {
		nameField := v.FieldByName("Name")
		if v.FieldByName("Value").String() != "" && nameField.String() == "" &&
			len(path) > 0 && nameField.CanSet() {
			nameField.SetString(derivedTokenName(path[len(path)-1]))
		}
		return
	}
	// Stroke, LineHeight, LetterSpacing and Opacity are optional too: a
	// fully-unset token stays zero here, and the emitter and token map
	// give it the default theme's value.
	if isOptionalStringToken(v.Type()) {
		nameField := v.FieldByName("Name")
		if v.FieldByName("Value").String() != "" && nameField.String() == "" &&
			len(path) > 0 && nameField.CanSet() {
			nameField.SetString(derivedTokenName(path[len(path)-1]))
		}
		return
	}
	// Token leaf? Fill Name if empty.
	switch v.Type() {
	case reflect.TypeFor[Color](), reflect.TypeFor[Spacing](),
		reflect.TypeFor[Radius](), reflect.TypeFor[Font](),
		reflect.TypeFor[Breakpoint](), reflect.TypeFor[Shadow](),
		reflect.TypeFor[ZIndexValue](), reflect.TypeFor[Duration](),
		reflect.TypeFor[Easing](), reflect.TypeFor[FontSize](),
		reflect.TypeFor[Size](), reflect.TypeFor[FontWeight]():
		nameField := v.FieldByName("Name")
		if !nameField.IsValid() || nameField.String() != "" {
			return
		}
		if len(path) == 0 || !nameField.CanSet() {
			return
		}
		nameField.SetString(derivedTokenName(path[len(path)-1]))
		return
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		fieldName := v.Type().Field(i).Name
		// Skip the bookkeeping `Name string` on Theme itself.
		if fieldName == "Name" && f.Kind() == reflect.String {
			continue
		}
		autofillTokens(f, append(path, fieldName))
	}
}

// derivedTokenName maps a Go struct-field name to its canonical token
// name. The size-scale steps spell their ALL-CAPS runs numerically the
// way DefaultTheme, the framework's own CSS and the docs already read
// them (XXL → "2xl", XXXL → "3xl"); everything else is camelToKebab.
// One table at the single derivation site keeps auto-named themes and
// DefaultTheme from emitting two different spellings of one token
// (TestAutoFillNamesDeriveCanonicalNames pins them equal).
func derivedTokenName(field string) string {
	switch field {
	case "XXL":
		return "2xl"
	case "XXXL":
		return "3xl"
	default:
		return camelToKebab(field)
	}
}

// camelToKebab converts "PrimaryFg" → "primary-fg", "XXXL" → "xxxl",
// "XS" → "xs", "Background" → "background". Handles ALL-CAPS runs as
// a single segment so canonical acronyms don't sprout dashes between
// every letter.
func camelToKebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		isUpper := r >= 'A' && r <= 'Z'
		if i > 0 && isUpper {
			// Insert dash unless the previous letter is also upper
			// (we're inside an acronym like XL / XXL).
			prev := rune(s[i-1])
			if !(prev >= 'A' && prev <= 'Z') {
				b.WriteByte('-')
			}
		}
		if isUpper {
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Validate walks every typed token field of the theme and ensures
// each has a non-empty Name + a non-zero Value. Returns the first
// field path that fails, naming the missing piece, so authors see:
//
//	theme.Colors.Primary: Color.Name is empty
//
// The Components map is checked with the same intent: a key that is
// not lowercase dot-separated words, or a value that is not one
// lowercase word, is a theme-shape mistake and fails here, at boot.
//
// Finally the filled-control ink pairs (primary × primary-fg, danger ×
// danger-fg) must clear 4.5:1 when both values are plain hex, in the
// light palette and, key by key with light fallback, in a non-empty
// DarkColors map — see validatePairContrast.
//
// MustValidate is the panicking variant used by App.WithTheme so a
// bad theme fails at boot, not at first request.
func (t Theme) Validate() error {
	if err := validateTokens(reflect.ValueOf(t), "Theme"); err != nil {
		return err
	}
	if err := duplicateTokenKey(t); err != nil {
		return err
	}
	if err := validateComponents(t.Components); err != nil {
		return err
	}
	if err := validateKnobs(t.Knobs); err != nil {
		return err
	}
	return t.validatePairContrast()
}

// knobKeyPattern is a --ui-* variable name without its dashes.
var knobKeyPattern = regexp.MustCompile(`^ui-[a-z0-9]+(-[a-z0-9]+)*$`)

// validateKnobs refuses a knob whose key is not a ui-* name or whose
// value could break out of its declaration. Keys are judged before
// values so the error names the first bad key in sorted order.
func validateKnobs(knobs map[string]string) error {
	for _, k := range sortedMapKeys(knobs) {
		if !knobKeyPattern.MatchString(k) {
			return fmt.Errorf("Theme.Knobs[%q]: a knob key is ui- followed by lower-case words joined by single dashes", k)
		}
		if err := validateFreeFormCSS(knobs[k]); err != nil {
			return fmt.Errorf("Theme.Knobs[%q]: %w", k, err)
		}
	}
	return nil
}

// isOptionalStringToken reports whether t is one of the optional
// string-valued token types: left fully zero, it is neither named,
// validated nor emitted, and the kit's var() fallback applies.
func isOptionalStringToken(t reflect.Type) bool {
	switch t {
	case reflect.TypeFor[Stroke](), reflect.TypeFor[LineHeight](),
		reflect.TypeFor[LetterSpacing](), reflect.TypeFor[Opacity]():
		return true
	}
	return false
}

// validateOptionalToken is the shared check for an optional string
// token: fully unset passes, a value with no name is an autofill miss,
// and a set value must pass its type's grammar.
func validateOptionalToken(path, typ, name, value string, check func(string) error) error {
	if value == "" && name == "" {
		return nil
	}
	if name == "" {
		return fmt.Errorf("%s: %s.Name is empty (Value=%q). Run AutoFillNames or set the Name", path, typ, value)
	}
	if err := check(value); err != nil {
		return fmt.Errorf("%s: %s.Value (Name=%q): %w", path, typ, name, err)
	}
	return nil
}

// validatePairContrast refuses a theme whose filled-control ink pairs —
// primary × primary-fg and danger × danger-fg — sit below the 4.5:1 WCAG
// AA floor. Those are the pairs the token system actually guarantees and
// the filled button paints; borrowing another variant's ink was exactly
// how the amber-primary sites shipped unreadable danger buttons (an axe
// failure out of the box). The check only runs when BOTH values parse as
// plain hex (#RGB / #RRGGBB): oklch(), var() references, rgb()/hsl() and
// named colours are skipped rather than approximated, so a theme whose
// colours Go cannot resolve exactly is never refused on a guess.
//
// A non-empty DarkColors map re-declares the same variables, so each pair
// is resolved from it and judged by the same floor: an absent key falls
// back to the light token, the value that actually paints when the map
// does not override it. The partial-map case this catches (a dark colour
// whose light ink cannot wear it) is the filled-control half of what the
// DarkPaletteGaps boot warning names.
func (t Theme) validatePairContrast() error {
	for _, p := range []struct {
		name   string
		bg, fg Color
	}{
		{"primary", t.Colors.Primary, t.Colors.PrimaryFg},
		{"danger", t.Colors.Danger, t.Colors.DangerFg},
	} {
		if err := inkPairError("Theme.Colors", p.name, p.bg.Value, p.fg.Value); err != nil {
			return err
		}
	}
	if len(t.DarkColors) == 0 {
		return nil
	}
	for _, p := range []struct {
		name   string
		bg, fg string // the light values: the fallback per key
	}{
		{"primary", t.Colors.Primary.Value, t.Colors.PrimaryFg.Value},
		{"danger", t.Colors.Danger.Value, t.Colors.DangerFg.Value},
	} {
		bg, fg := p.bg, p.fg
		if v, ok := t.DarkColors[p.name]; ok {
			bg = v
		}
		if v, ok := t.DarkColors[p.name+"-fg"]; ok {
			fg = v
		}
		if err := inkPairError("Theme.DarkColors", p.name, bg, fg); err != nil {
			return err
		}
	}
	return nil
}

// inkPairError is the one pair check both schemes share: nil when the
// pair clears the floor or cannot be computed exactly, the refusal
// otherwise. where names the palette the values came from.
func inkPairError(where, name, bg, fg string) error {
	ratio, ok := contrastRatio(fg, bg)
	if !ok || ratio >= 4.5 {
		return nil
	}
	return fmt.Errorf(
		"%s: %s × %s-fg contrast is %.2f:1 (%s on %s), below the 4.5:1 WCAG AA floor the filled %s button paints; give %s-fg an ink that clears AA over %s",
		where, name, name, ratio, fg, bg, name, name, name)
}

// MustValidate panics if validation fails. Wraps Validate.
func (t Theme) MustValidate() {
	if err := t.Validate(); err != nil {
		panic("style.Theme: invalid: " + err.Error())
	}
}

func validateTokens(v reflect.Value, path string) error {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			if err := validateTokens(v.Index(i), extensionPath(v.Index(i))); err != nil {
				return err
			}
		}
		return nil
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	// Typed token leaves, check fields are populated.
	switch tk := v.Interface().(type) {
	case Color:
		if tk.Name == "" {
			return fmt.Errorf("%s: Color.Name is empty", path)
		}
		if tk.Value == "" {
			return fmt.Errorf("%s: Color.Value is empty (Name=%q)", path, tk.Name)
		}
		return nil
	case Spacing:
		if tk.Name == "" {
			return fmt.Errorf("%s: Spacing.Name is empty", path)
		}
		// Allow xs/sm zero only if the name is "0" or "none"; a
		// scaled token (md/lg/xl) at Value=0 is almost always a
		// configuration mistake.
		if tk.Value == 0 && tk.Name != "none" && tk.Name != "0" {
			return fmt.Errorf("%s: Spacing.Value is 0 (Name=%q). It emits `--spacing-%s: 0px;` and breaks layout", path, tk.Name, tk.Name)
		}
		return nil
	case Radius:
		if tk.Name == "" {
			return fmt.Errorf("%s: Radius.Name is empty", path)
		}
		// 0 is a real radius on any step: a square theme sets them all
		// to it. Only a negative value is broken.
		if tk.Value < 0 {
			return fmt.Errorf("%s: Radius.Value is negative (%d, Name=%q)", path, tk.Value, tk.Name)
		}
		return nil
	case Stroke:
		return validateOptionalToken(path, "Stroke", tk.Name, tk.Value, validateStrokeValue)
	case LineHeight:
		return validateOptionalToken(path, "LineHeight", tk.Name, tk.Value, validateLineHeightValue)
	case LetterSpacing:
		return validateOptionalToken(path, "LetterSpacing", tk.Name, tk.Value, validateLetterSpacingValue)
	case Opacity:
		return validateOptionalToken(path, "Opacity", tk.Name, tk.Value, validateOpacityValue)
	case Font:
		if tk.Name == "" {
			return fmt.Errorf("%s: Font.Name is empty", path)
		}
		if tk.Value == "" {
			return fmt.Errorf("%s: Font.Value is empty (Name=%q)", path, tk.Name)
		}
		return nil
	case Breakpoint:
		if tk.Name == "" {
			return fmt.Errorf("%s: Breakpoint.Name is empty", path)
		}
		if tk.Value <= 0 {
			return fmt.Errorf("%s: Breakpoint.Value must be > 0 (Name=%q)", path, tk.Name)
		}
		return nil
	case Shadow:
		if tk.Name == "" {
			return fmt.Errorf("%s: Shadow.Name is empty", path)
		}
		if tk.Value == "" {
			return fmt.Errorf("%s: Shadow.Value is empty (Name=%q)", path, tk.Name)
		}
		return nil
	case ZIndexValue:
		if tk.Name == "" {
			return fmt.Errorf("%s: ZIndex.Name is empty", path)
		}
		// Z-index 0 is legitimate (base layer); negative is also
		// valid CSS. Only flag the all-zero default that signals
		// "I forgot to set this".
		if tk.Value == 0 && tk.Name != "base" && tk.Name != "0" {
			return fmt.Errorf("%s: ZIndex.Value is 0 (Name=%q). Use Name \"base\"/\"0\" if intentional", path, tk.Name)
		}
		return nil
	case Duration:
		if tk.Name == "" {
			return fmt.Errorf("%s: Duration.Name is empty", path)
		}
		if tk.Value <= 0 {
			return fmt.Errorf("%s: Duration.Value must be > 0 (Name=%q)", path, tk.Name)
		}
		return nil
	case Easing:
		if tk.Name == "" {
			return fmt.Errorf("%s: Easing.Name is empty", path)
		}
		if tk.Value == "" {
			return fmt.Errorf("%s: Easing.Value is empty (Name=%q)", path, tk.Name)
		}
		return nil
	case FontSize:
		if tk.Name == "" {
			return fmt.Errorf("%s: FontSize.Name is empty", path)
		}
		if tk.Value == "" {
			return fmt.Errorf("%s: FontSize.Value is empty (Name=%q)", path, tk.Name)
		}
		return nil
	case Size:
		if tk.Name == "" {
			return fmt.Errorf("%s: Size.Name is empty", path)
		}
		if err := validateSizeValue(tk.Value); err != nil {
			return fmt.Errorf("%s: Size.Value (Name=%q): %w", path, tk.Name, err)
		}
		return nil
	case FontWeight:
		if tk.Name == "" {
			return fmt.Errorf("%s: FontWeight.Name is empty", path)
		}
		if err := validateFontWeight(tk.Value); err != nil {
			return fmt.Errorf("%s: FontWeight.Value (Name=%q): %w", path, tk.Name, err)
		}
		return nil
	case CodeColor:
		// Optional group: an entirely-unset token is fine (component
		// CSS keeps its built-in fallback palette). Only a half-set
		// token is a configuration mistake.
		if tk.Value == "" && tk.Name != "" {
			return fmt.Errorf("%s: CodeColor.Value is empty (Name=%q). Leave the token fully zero to fall back, or give it a value", path, tk.Name)
		}
		if tk.Value != "" && tk.Name == "" {
			return fmt.Errorf("%s: CodeColor.Name is empty (Value=%q). Run AutoFillNames or set the Name", path, tk.Value)
		}
		return nil
	}
	// Recurse into struct fields.
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		fieldName := v.Type().Field(i).Name
		// Skip the bookkeeping `Name string` on Theme itself.
		if fieldName == "Name" && f.Kind() == reflect.String {
			continue
		}
		if err := validateTokens(f, path+"."+fieldName); err != nil {
			return err
		}
	}
	return nil
}

// DefaultTheme returns a fully-populated baseline theme suitable
// for every framework/ui component. The scaffold (`gofastr theme
// init`) writes this as the user's starting theme.go.
func DefaultTheme() Theme {
	return Theme{
		Name: "default",
		Colors: ColorSet{
			// A neutral zinc palette: near-black primary on a white page,
			// one hairline border, one soft surface for hover and fills.
			// Brand colour is the host's call (theme.Overrides.Primary).
			Primary: Color{Name: "primary", Value: "#18181B"},
			// Pure white, not an off-white: a host that overrides only
			// Primary keeps this ink, and #FAFAFA dropped brand colours that
			// clear AA under white below 4.5:1.
			PrimaryFg:    Color{Name: "primary-fg", Value: "#FFFFFF"},
			Secondary:    Color{Name: "secondary", Value: "#F4F4F5"},
			SecondaryFg:  Color{Name: "secondary-fg", Value: "#18181B"},
			Background:   Color{Name: "background", Value: "#FFFFFF"},
			Surface:      Color{Name: "surface", Value: "#FFFFFF"},
			SurfaceSoft:  Color{Name: "surface-soft", Value: "#F4F4F5"},
			Text:         Color{Name: "text", Value: "#09090B"},
			TextMuted:    Color{Name: "text-muted", Value: "#52525B"},
			TextSubtle:   Color{Name: "text-subtle", Value: "#71717A"}, // 4.55:1 on surface, was #A1A1AA (2.56:1, fails AA); also the focus ring
			Border:       Color{Name: "border", Value: "#E4E4E7"},
			BorderStrong: Color{Name: "border-strong", Value: "#D4D4D8"},
			// Status tones are used two ways by framework/ui components:
			// as WHITE-TEXT FILLS (toasts, button--danger) and as LABEL
			// TEXT on their own 15%-tinted chips (Badge, Tag, StatCard
			// trend, ValidationSummary). The tint is the harder target,
			// the previous values (#DC2626 / #15803D / #A16207 / #2563EB)
			// hit 4.5:1 on white but only 3.7–4.2:1 on the tinted chips,
			// which axe flags on any light scheme. These shades clear
			// 4.6:1 on the chips and ≥6.4:1 with white fills. Danger's
			// fill ink is its own token: danger-fg pairs with danger the
			// way primary-fg pairs with primary, so a light primary with
			// dark ink cannot leak that ink onto a filled danger button.
			Danger:   Color{Name: "danger", Value: "#B91C1C"},    // 5.2:1 on its 15% chip, was #DC2626 (3.96:1)
			DangerFg: Color{Name: "danger-fg", Value: "#FFFFFF"}, // 6.47:1 on danger, the filled danger button's ink
			Success:  Color{Name: "success", Value: "#166534"},   // 5.6:1 on its 15% chip, was #15803D (4.10:1)
			Warning:  Color{Name: "warning", Value: "#854D0E"},   // 5.4:1 on its 15% chip, was #A16207 (4.03:1)
			Info:     Color{Name: "info", Value: "#1D4ED8"},      // 5.3:1 on its 15% chip, was #2563EB (4.23:1)
			Accent:   Color{Name: "accent", Value: "#2563EB"},
			// Code surface: an always-dark panel for ui.CodeBlock and
			// other code-display contexts. Light mode keeps the dark
			// inkwell look (classic IDE feel); dark mode shifts it a
			// little deeper than the page surface so the code still
			// stands out from the body. Token names are referenced by
			// var(--color-code-*) in component CSS.
			CodeSurface: Color{Name: "code-surface", Value: "#18181B"},
			CodeText:    Color{Name: "code-text", Value: "#E4E4E7"},
			CodeBorder:  Color{Name: "code-border", Value: "#27272A"},
		},
		Spacing: SpacingScale{
			XS:   Spacing{Name: "xs", Value: 2},
			SM:   Spacing{Name: "sm", Value: 4},
			MD:   Spacing{Name: "md", Value: 8},
			LG:   Spacing{Name: "lg", Value: 16},
			XL:   Spacing{Name: "xl", Value: 24},
			XXL:  Spacing{Name: "2xl", Value: 32},
			XXXL: Spacing{Name: "3xl", Value: 48},
		},
		Radii: RadiusSet{
			None: Radius{Name: "none", Value: 0},
			SM:   Radius{Name: "sm", Value: 6},
			MD:   Radius{Name: "md", Value: 8},
			LG:   Radius{Name: "lg", Value: 10},
			XL:   Radius{Name: "xl", Value: 14},
			Full: Radius{Name: "full", Value: 9999},
		},
		Strokes: StrokeSet{
			Thin:        Stroke{Name: "thin", Value: "1px"},
			Thick:       Stroke{Name: "thick", Value: "2px"},
			Focus:       Stroke{Name: "focus", Value: "2px"},
			FocusOffset: Stroke{Name: "focus-offset", Value: "2px"},
		},
		Leading: LeadingSet{
			Tight:   LineHeight{Name: "tight", Value: "1.2"},
			Snug:    LineHeight{Name: "snug", Value: "1.4"},
			Normal:  LineHeight{Name: "normal", Value: "1.5"},
			Relaxed: LineHeight{Name: "relaxed", Value: "1.6"},
		},
		Tracking: TrackingSet{
			Tighter: LetterSpacing{Name: "tighter", Value: "-0.03em"},
			Tight:   LetterSpacing{Name: "tight", Value: "-0.02em"},
			Snug:    LetterSpacing{Name: "snug", Value: "-0.01em"},
			Wide:    LetterSpacing{Name: "wide", Value: "0.04em"},
			Wider:   LetterSpacing{Name: "wider", Value: "0.08em"},
		},
		Opacities: OpacitySet{
			Faint:    Opacity{Name: "faint", Value: "0.2"},
			Disabled: Opacity{Name: "disabled", Value: "0.5"},
			Muted:    Opacity{Name: "muted", Value: "0.6"},
		},
		Fonts: FontSet{
			Body:    Font{Name: "body", Value: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif"},
			Heading: Font{Name: "heading", Value: "ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Inter, Roboto, sans-serif"},
			Mono:    Font{Name: "mono", Value: "ui-monospace, 'SF Mono', Menlo, Consolas, 'JetBrains Mono', monospace"},
		},
		Breakpoints: BreakpointSet{
			SM:  Breakpoint{Name: "sm", Value: 640},
			MD:  Breakpoint{Name: "md", Value: 768},
			LG:  Breakpoint{Name: "lg", Value: 1024},
			XL:  Breakpoint{Name: "xl", Value: 1280},
			XXL: Breakpoint{Name: "2xl", Value: 1536},
		},
		Shadows: ShadowSet{
			None: Shadow{Name: "none", Value: "none"},
			XS:   Shadow{Name: "xs", Value: "0 1px 2px 0 rgba(0,0,0,0.05)"},
			SM:   Shadow{Name: "sm", Value: "0 1px 3px 0 rgba(0,0,0,0.10), 0 1px 2px -1px rgba(0,0,0,0.10)"},
			MD:   Shadow{Name: "md", Value: "0 4px 6px -1px rgba(0,0,0,0.10), 0 2px 4px -2px rgba(0,0,0,0.10)"},
			LG:   Shadow{Name: "lg", Value: "0 10px 15px -3px rgba(0,0,0,0.10), 0 4px 6px -4px rgba(0,0,0,0.10)"},
			XL:   Shadow{Name: "xl", Value: "0 20px 25px -5px rgba(0,0,0,0.10), 0 8px 10px -6px rgba(0,0,0,0.10)"},
		},
		ZIndex: ZIndexSet{
			Dropdown: ZIndexValue{Name: "dropdown", Value: 100},
			Sticky:   ZIndexValue{Name: "sticky", Value: 200},
			Modal:    ZIndexValue{Name: "modal", Value: 300},
			Popover:  ZIndexValue{Name: "popover", Value: 400},
			Toast:    ZIndexValue{Name: "toast", Value: 500},
		},
		Durations: DurationSet{
			Fast:   Duration{Name: "fast", Value: 150 * time.Millisecond},
			Normal: Duration{Name: "normal", Value: 250 * time.Millisecond},
			Slow:   Duration{Name: "slow", Value: 400 * time.Millisecond},

			OverlayEnter:  Duration{Name: "overlay-enter", Value: 200 * time.Millisecond},
			OverlayExit:   Duration{Name: "overlay-exit", Value: 160 * time.Millisecond},
			ToastEnter:    Duration{Name: "toast-enter", Value: 220 * time.Millisecond},
			ToastExit:     Duration{Name: "toast-exit", Value: 180 * time.Millisecond},
			DropdownEnter: Duration{Name: "dropdown-enter", Value: 120 * time.Millisecond},
		},
		Easings: EasingSet{
			EaseOut:   Easing{Name: "ease-out", Value: "cubic-bezier(0.16, 1, 0.3, 1)"},
			EaseIn:    Easing{Name: "ease-in", Value: "cubic-bezier(0.4, 0, 1, 1)"},
			EaseInOut: Easing{Name: "ease-in-out", Value: "cubic-bezier(0.4, 0, 0.2, 1)"},
			Spring:    Easing{Name: "spring", Value: "cubic-bezier(0.34, 1.56, 0.64, 1)"},
		},
		Typography: FontSizeSet{
			XS:   FontSize{Name: "xs", Value: "0.75rem"},
			SM:   FontSize{Name: "sm", Value: "0.875rem"},
			Base: FontSize{Name: "base", Value: "1rem"},
			LG:   FontSize{Name: "lg", Value: "1.125rem"},
			XL:   FontSize{Name: "xl", Value: "1.25rem"},
			XXL:  FontSize{Name: "2xl", Value: "1.5rem"},
			XXXL: FontSize{Name: "3xl", Value: "1.875rem"},
		},
		FontWeights: FontWeightSet{
			Normal:   FontWeight{Name: "normal", Value: 400},
			Medium:   FontWeight{Name: "medium", Value: 500},
			Semibold: FontWeight{Name: "semibold", Value: 600},
			Bold:     FontWeight{Name: "bold", Value: 700},
		},
		Layout: LayoutSet{
			TouchTarget:  Spacing{Name: "touch-target", Value: 44},
			PageWidth:    Size{Name: "page-width", Value: "66rem"},
			PageGutter:   Size{Name: "page-gutter", Value: "clamp(20px, 5vw, 32px)"},
			HeaderHeight: Size{Name: "header-height", Value: "56px"},
			NarrowWidth:  Size{Name: "narrow-width", Value: "640px"},
			ContentWidth: Size{Name: "content-width", Value: "1080px"},
			WideWidth:    Size{Name: "wide-width", Value: "1280px"},
		},
		// Syntax-highlight palette (--tk-*). These are the values the
		// ui.CodeBlock CSS previously carried only as var() fallbacks,
		// promoted to theme slots so dark mode (Theme.DarkCode) and
		// re-skins can restyle code blocks. Tuned for the always-dark
		// default CodeSurface, so they hold in both page schemes. PN
		// chains to the code text color, matching the old `inherit`
		// fallback behavior.
		Code: CodeSet{
			KW:  CodeColor{Name: "kw", Value: "#C792EA"},
			FN:  CodeColor{Name: "fn", Value: "#82AAFF"},
			Str: CodeColor{Name: "str", Value: "#C3E88D"},
			Num: CodeColor{Name: "num", Value: "#F78C6C"},
			// Comments must hold ≥4.5:1 on the always-dark CodeSurface
			// (#18181B): the classic #676E95 measures 3.6:1 and fails
			// WCAG AA / axe; #8C93B0 keeps the muted blue-grey at 5.8:1.
			Com:  CodeColor{Name: "com", Value: "#8C93B0"},
			Type: CodeColor{Name: "type", Value: "#FFCB6B"},
			PN:   CodeColor{Name: "pn", Value: "var(--color-code-text)"},
		},
	}
}
