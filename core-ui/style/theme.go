package style

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// Theme is the canonical typed design system.
type Theme struct {
	Name string

	// DarkColors is the dark-scheme palette, keyed by color token name
	// ("background", "surface", "text", "primary", …).
	DarkColors map[string]string

	// DarkCode is the dark-scheme syntax palette, keyed by code token name ("kw",
	// "str", …).
	DarkCode map[string]string

	// Components is the canonical flattened component-option set, keyed like
	// "density" or "button.treatment" with one lowercase word as the value.
	Components map[string]string

	// Knobs sets the per-component --ui-* variables from the theme, keyed without
	// the leading dashes ("ui-sidebar-width".
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

	// Extensions holds the app's own token sets, added with Extend.
	Extensions []any
}

// ColorSet is the canonical palette.
type ColorSet struct {
	Primary, PrimaryFg               Color
	Secondary, SecondaryFg           Color
	Background, Surface, SurfaceSoft Color
	Text, TextMuted, TextSubtle      Color
	Border, BorderStrong             Color
	// Danger is the one status tone whose filled-control ink is its own token
	// (danger-fg) rather than primary-fg.
	Danger, DangerFg       Color
	Success, Warning, Info Color
	Accent                 Color

	// Code surface, the background + foreground used by code-display components
	// (ui.CodeBlock, demo source panels).
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

// StrokeSet.
type StrokeSet struct {
	Thin, Thick, Focus, FocusOffset Stroke
}

// LeadingSet.
type LeadingSet struct {
	Tight, Snug, Normal, Relaxed LineHeight
}

// TrackingSet.
type TrackingSet struct {
	Tighter, Tight, Snug, Wide, Wider LetterSpacing
}

// OpacitySet.
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

// ShadowSet.
type ShadowSet struct {
	None, XS, SM, MD, LG, XL Shadow
}

// ZIndexSet.
type ZIndexSet struct {
	Dropdown, Sticky, Modal, Popover, Toast ZIndexValue
}

// DurationSet.
type DurationSet struct {
	Fast, Normal, Slow Duration

	// OverlayEnter is how long modal/drawer surfaces take to slide or fade in.
	OverlayEnter, OverlayExit Duration

	// ToastEnter / ToastExit cover slide-in and dismiss of toast items.
	ToastEnter, ToastExit Duration

	// DropdownEnter covers fade/scale-in for anchored dropdown menus.
	DropdownEnter Duration
}

// EasingSet.
type EasingSet struct {
	// EaseOut decelerates, the default for elements entering view.
	EaseOut, EaseIn, EaseInOut, Spring Easing
}

// FontSizeSet: typography size scale.
type FontSizeSet struct {
	XS, SM, Base, LG, XL, XXL, XXXL FontSize
}

// CodeSet.
type CodeSet struct {
	KW, FN, Str, Num, Com, Type, PN CodeColor
}

// FontWeightSet: the weights a design uses, by role.
type FontWeightSet struct {
	Normal, Medium, Semibold, Bold FontWeight
}

// LayoutSet.
type LayoutSet struct {
	TouchTarget Spacing

	PageWidth, PageGutter, HeaderHeight Size

	NarrowWidth, ContentWidth, WideWidth Size
}

// AutoFillNames walks every typed token field of t and, for any token whose
// Name is empty, assigns the canonical token name derived from the Go struct-
// field path.
func AutoFillNames(t *Theme) {
	autofillTokens(reflect.ValueOf(t).Elem(), nil)
}

func autofillTokens(v reflect.Value, path []string) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			autofillTokens(v.Index(i), nil)
		}
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	if v.Type() == reflect.TypeFor[CodeColor]() || isOptionalStringToken(v.Type()) {
		autofillName(v, path)
		return
	}
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
		if fieldName == "Name" && f.Kind() == reflect.String {
			continue
		}
		autofillTokens(f, append(path, fieldName))
	}
}

func autofillName(v reflect.Value, path []string) {
	nameField := v.FieldByName("Name")
	if v.FieldByName("Value").String() != "" && nameField.String() == "" &&
		len(path) > 0 && nameField.CanSet() {
		nameField.SetString(derivedTokenName(path[len(path)-1]))
	}
}
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

func camelToKebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		isUpper := r >= 'A' && r <= 'Z'
		if i > 0 && isUpper {
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

// Validate walks every typed token field of the theme and ensures each has a
// non-empty Name + a non-zero Value.
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

var knobKeyPattern = regexp.MustCompile(`^ui-[a-z0-9]+(-[a-z0-9]+)*$`)

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

func isOptionalStringToken(t reflect.Type) bool {
	switch t {
	case reflect.TypeFor[Stroke](), reflect.TypeFor[LineHeight](),
		reflect.TypeFor[LetterSpacing](), reflect.TypeFor[Opacity]():
		return true
	}
	return false
}

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
		bg, fg string
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

func inkPairError(where, name, bg, fg string) error {
	if !isPlainHex(bg) || !isPlainHex(fg) {
		return nil
	}
	ratio, ok := contrastRatio(fg, bg)
	if !ok || ratio >= 4.5 {
		return nil
	}
	return fmt.Errorf(
		"%s: %s × %s-fg contrast is %.2f:1 (%s on %s), below the 4.5:1 WCAG AA floor the filled %s button paints; give %s-fg an ink that clears AA over %s",
		where, name, name, ratio, fg, bg, name, name, name)
}

// ControlBoundaryFloor is the WCAG 1.4.11 non-text contrast floor a form
// control's boundary must clear against the surface it sits on.
const ControlBoundaryFloor = 3.0

var paletteRef = regexp.MustCompile(`^var\(\s*--color-([a-z0-9-]+)\s*(?:,[^)]*)?\)$`)

func ContrastWarnings(t Theme) []string {
	light := map[string]string{}
	for k, v := range ThemeToTokens(t) {
		if name, ok := strings.CutPrefix(k, "color-"); ok {
			light[name] = v
		}
	}
	edge := "text-subtle"
	if v := strings.TrimSpace(t.Knobs["ui-control-border"]); v != "" {
		if m := paletteRef.FindStringSubmatch(v); m != nil {
			edge = m[1]
		} else {
			light["ui-control-border"] = v
			edge = "ui-control-border"
		}
	}
	schemes := []struct {
		where  string
		lookup func(string) string
	}{{"light", func(k string) string { return light[k] }}}
	if len(t.DarkColors) > 0 {
		schemes = append(schemes, struct {
			where  string
			lookup func(string) string
		}{"dark", func(k string) string {
			if v, ok := t.DarkColors[k]; ok {
				return v
			}
			return light[k]
		}})
	}
	var out []string
	for _, sc := range schemes {
		for _, p := range []struct {
			fg, bg     string
			floor      float64
			hexRefused bool
		}{
			{"primary-fg", "primary", 4.5, true},
			{"danger-fg", "danger", 4.5, true},
			{edge, "surface", ControlBoundaryFloor, false},
		} {
			fg, bg := sc.lookup(p.fg), sc.lookup(p.bg)
			if p.hexRefused && isPlainHex(fg) && isPlainHex(bg) {
				continue
			}
			if ratio, ok := contrastRatio(fg, bg); ok && ratio < p.floor {
				out = append(out, fmt.Sprintf("%s: %s %s on %s %s is %.2f:1, below %.1f:1", sc.where, p.fg, fg, p.bg, bg, ratio, p.floor))
			}
		}
	}
	return out
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
		if tk.Value == 0 && tk.Name != "none" && tk.Name != "0" {
			return fmt.Errorf("%s: Spacing.Value is 0 (Name=%q). It emits `--spacing-%s: 0px;` and breaks layout", path, tk.Name, tk.Name)
		}
		return nil
	case Radius:
		if tk.Name == "" {
			return fmt.Errorf("%s: Radius.Name is empty", path)
		}
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
		if tk.Value == "" && tk.Name != "" {
			return fmt.Errorf("%s: CodeColor.Value is empty (Name=%q). Leave the token fully zero to fall back, or give it a value", path, tk.Name)
		}
		if tk.Value != "" && tk.Name == "" {
			return fmt.Errorf("%s: CodeColor.Name is empty (Value=%q). Run AutoFillNames or set the Name", path, tk.Value)
		}
		return nil
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		fieldName := v.Type().Field(i).Name
		if fieldName == "Name" && f.Kind() == reflect.String {
			continue
		}
		if err := validateTokens(f, path+"."+fieldName); err != nil {
			return err
		}
	}
	return nil
}

// DefaultDarkColors returns the canonical dark palette, keyed by token name
// the way Theme.DarkColors is.
func DefaultDarkColors() map[string]string {
	return map[string]string{
		"accent":        "#60A5FA",
		"background":    "#09090B",
		"border":        "#27272A",
		"border-strong": "#3F3F46",
		"code-border":   "#27272A",
		"code-surface":  "#18181B",
		"code-text":     "#E4E4E7",
		"danger":        "#F87171",
		"danger-fg":     "#111827",
		"info":          "#60A5FA",
		"primary":       "#FAFAFA",
		"primary-fg":    "#18181B",
		"secondary":     "#27272A",
		"secondary-fg":  "#FAFAFA",
		"success":       "#4ADE80",
		"surface":       "#18181B",
		"surface-soft":  "#27272A",
		"text":          "#FAFAFA",
		"text-muted":    "#D4D4D8",
		"text-subtle":   "#A1A1AA",
		"warning":       "#FBBF24",
	}
}

// DefaultTheme returns a fully-populated baseline theme suitable for every
// framework/ui component.
func DefaultTheme() Theme {
	return Theme{
		Name: "default",
		Colors: ColorSet{
			Primary:      Color{Name: "primary", Value: "#18181B"},
			PrimaryFg:    Color{Name: "primary-fg", Value: "#FFFFFF"},
			Secondary:    Color{Name: "secondary", Value: "#F4F4F5"},
			SecondaryFg:  Color{Name: "secondary-fg", Value: "#18181B"},
			Background:   Color{Name: "background", Value: "#FFFFFF"},
			Surface:      Color{Name: "surface", Value: "#FFFFFF"},
			SurfaceSoft:  Color{Name: "surface-soft", Value: "#F4F4F5"},
			Text:         Color{Name: "text", Value: "#09090B"},
			TextMuted:    Color{Name: "text-muted", Value: "#52525B"},
			TextSubtle:   Color{Name: "text-subtle", Value: "#71717A"},
			Border:       Color{Name: "border", Value: "#E4E4E7"},
			BorderStrong: Color{Name: "border-strong", Value: "#D4D4D8"},
			Danger:       Color{Name: "danger", Value: "#B91C1C"},
			DangerFg:     Color{Name: "danger-fg", Value: "#FFFFFF"},
			Success:      Color{Name: "success", Value: "#166534"},
			Warning:      Color{Name: "warning", Value: "#854D0E"},
			Info:         Color{Name: "info", Value: "#1D4ED8"},
			Accent:       Color{Name: "accent", Value: "#2563EB"},
			CodeSurface:  Color{Name: "code-surface", Value: "#18181B"},
			CodeText:     Color{Name: "code-text", Value: "#E4E4E7"},
			CodeBorder:   Color{Name: "code-border", Value: "#27272A"},
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
			Spring:    Easing{Name: "spring", Value: "linear(0, 0.281 6.7%, 0.723 12.9%, 0.938 16.7%, 1.077 20.4%, 1.149 24.3%, 1.163 27.6%, 1.154 29.9%, 1.129 32.8%, 1.051 39.6%, 1.017 43.1%, 0.977 51%, 0.974 53.8%, 0.975 57.1%, 0.997 69.8%, 1.003 76.9%, 1)"},
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
		Code: CodeSet{
			KW:   CodeColor{Name: "kw", Value: "#C792EA"},
			FN:   CodeColor{Name: "fn", Value: "#82AAFF"},
			Str:  CodeColor{Name: "str", Value: "#C3E88D"},
			Num:  CodeColor{Name: "num", Value: "#F78C6C"},
			Com:  CodeColor{Name: "com", Value: "#8C93B0"},
			Type: CodeColor{Name: "type", Value: "#FFCB6B"},
			PN:   CodeColor{Name: "pn", Value: "var(--color-code-text)"},
		},
	}
}
