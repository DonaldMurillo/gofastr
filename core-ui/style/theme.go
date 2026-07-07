package style

import (
	"fmt"
	"reflect"
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
	Name string // theme identifier — used for telemetry and the class-scoped block name

	// DarkColors is the dark-scheme palette, keyed by color token name
	// ("background", "surface", "text", "primary", …). When non-empty,
	// CSSCustomProperties emits a `:root[data-color-scheme="dark"]` block (and a
	// matching prefers-color-scheme fallback) re-declaring these tokens, so a
	// ui.ThemeToggle / the color-scheme bootstrap recolors the whole app — and
	// every surface that emits the theme CSS — by flipping one attribute. Empty
	// by default: an app opts into dark mode by supplying it, so existing
	// light-only apps are never surprised into dark by an OS preference. It's a
	// map (not a typed ColorSet) so the reflection token-walk ignores it.
	DarkColors map[string]string

	// DarkCode is the dark-scheme syntax palette, keyed by code token
	// name ("kw", "str", …). Same contract as DarkColors — when
	// non-empty its `--tk-<name>` re-declarations join the dark-scheme
	// blocks, so a theme toggle restyles code blocks along with the
	// rest of the page. Empty by default. A map (not a typed CodeSet)
	// so the reflection token-walk ignores it.
	DarkCode map[string]string

	Colors      ColorSet
	Spacing     SpacingScale
	Radii       RadiusSet
	Fonts       FontSet
	Breakpoints BreakpointSet
	Shadows     ShadowSet
	ZIndex      ZIndexSet
	Durations   DurationSet
	Easings     EasingSet
	Typography  FontSizeSet
	Layout      LayoutSet
	Code        CodeSet
}

// ColorSet is the canonical palette. Every theme must declare every
// field; framework/ui components reference them by name.
type ColorSet struct {
	Primary, PrimaryFg               Color
	Secondary, SecondaryFg           Color
	Background, Surface, SurfaceSoft Color
	Text, TextMuted, TextSubtle      Color
	Border, BorderStrong             Color
	Danger, Success, Warning, Info   Color
	Accent                           Color

	// Code surface — the background + foreground used by code-display
	// components (ui.CodeBlock, demo source panels). Intentionally a
	// SEPARATE token pair from Surface/Text so dark mode can keep code
	// blocks legibly distinct from the page background without
	// inverting (light text on dark background works in both schemes).
	CodeSurface, CodeText, CodeBorder Color
}

// SpacingScale — pixel-valued spacing scale.
type SpacingScale struct {
	XS, SM, MD, LG, XL, XXL, XXXL Spacing
}

// RadiusSet — border-radius scale.
type RadiusSet struct {
	None, SM, MD, LG, XL, Full Radius
}

// FontSet — font-family stacks.
type FontSet struct {
	Body, Heading, Mono Font
}

// BreakpointSet — viewport-width thresholds, in pixels.
type BreakpointSet struct {
	SM, MD, LG, XL, XXL Breakpoint
}

// ShadowSet — box-shadow depth scale.
type ShadowSet struct {
	None, SM, MD, LG, XL Shadow
}

// ZIndexSet — named layers. Prevents the `z-index: 9999` arms race
// — every elevated surface picks a layer by name.
type ZIndexSet struct {
	Dropdown, Sticky, Modal, Popover, Toast ZIndexValue
}

// DurationSet — animation / transition timing scale.
//
// The generic Fast/Normal/Slow are everyday tokens. The named overlay
// and toast/dropdown values are referenced by core-ui widget chrome so
// every modal, drawer, dropdown, and toast respects the same motion
// budget — and a single override on the theme retunes them all.
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

// EasingSet — CSS timing-function tokens. Widget chrome references
// these so motion curves stay theme-driven.
type EasingSet struct {
	// EaseOut decelerates — the default for elements entering view.
	// EaseIn accelerates — used when elements leave view.
	// EaseInOut for symmetric transitions. Spring overshoots slightly.
	EaseOut, EaseIn, EaseInOut, Spring Easing
}

// FontSizeSet — typography size scale.
type FontSizeSet struct {
	XS, SM, Base, LG, XL, XXL, XXXL FontSize
}

// CodeSet — the syntax-highlight palette consumed by code-display
// components via `--tk-<name>`. Field names ARE the emitted suffixes
// (KW → --tk-kw): KW keywords, FN function names, Str strings, Num
// numeric literals, Com comments, Type type names, PN punctuation.
// The group is optional — zero tokens are skipped (component CSS
// keeps its built-in fallback palette), so themes that never set it
// behave exactly as before the group existed. Dark-scheme values go
// in Theme.DarkCode.
type CodeSet struct {
	KW, FN, Str, Num, Com, Type, PN CodeColor
}

// LayoutSet — interaction-affordance dimensions. TouchTarget is
// the WCAG 2.5.5 minimum tap target (default 44px); buttons and
// form inputs reference var(--spacing-touch-target) to land on it.
type LayoutSet struct {
	TouchTarget Spacing
}

// AutoFillNames walks every typed token field of t and, for any
// token whose Name is empty, assigns it from the Go struct-field
// path in kebab-case. Authors can write
//
//	t.Colors.Primary = style.Color{Value: "#FF0000"}
//
// — the Name "primary" is filled in automatically. Explicit Name
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
// Spacing, …) with an empty Name, it assigns a kebab-case name
// derived from the most-recent struct field name visited.
//
// path[len-1] is the immediate field name (e.g. "Primary"); the
// kebab-case of that is the canonical CSS variable suffix.
func autofillTokens(v reflect.Value, path []string) {
	if v.Kind() != reflect.Struct {
		return
	}
	// CodeColor is the one OPTIONAL token type: a fully-unset token
	// stays zero (skipped by validation + emission, component CSS
	// falls back), so only autofill the Name once a Value was set.
	if v.Type() == reflect.TypeOf(CodeColor{}) {
		nameField := v.FieldByName("Name")
		if v.FieldByName("Value").String() != "" && nameField.String() == "" &&
			len(path) > 0 && nameField.CanSet() {
			nameField.SetString(camelToKebab(path[len(path)-1]))
		}
		return
	}
	// Token leaf? Fill Name if empty.
	switch v.Type() {
	case reflect.TypeOf(Color{}), reflect.TypeOf(Spacing{}),
		reflect.TypeOf(Radius{}), reflect.TypeOf(Font{}),
		reflect.TypeOf(Breakpoint{}), reflect.TypeOf(Shadow{}),
		reflect.TypeOf(ZIndexValue{}), reflect.TypeOf(Duration{}),
		reflect.TypeOf(Easing{}), reflect.TypeOf(FontSize{}):
		nameField := v.FieldByName("Name")
		if !nameField.IsValid() || nameField.String() != "" {
			return
		}
		if len(path) == 0 || !nameField.CanSet() {
			return
		}
		nameField.SetString(camelToKebab(path[len(path)-1]))
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
// MustValidate is the panicking variant used by App.WithTheme so a
// bad theme fails at boot, not at first request.
func (t Theme) Validate() error {
	return validateTokens(reflect.ValueOf(t), "Theme")
}

// MustValidate panics if validation fails. Wraps Validate.
func (t Theme) MustValidate() {
	if err := t.Validate(); err != nil {
		panic("style.Theme: invalid — " + err.Error())
	}
}

func validateTokens(v reflect.Value, path string) error {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	// Typed token leaves — check fields are populated.
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
			return fmt.Errorf("%s: Spacing.Value is 0 (Name=%q) — emits `--spacing-%s: 0px;` and breaks layout", path, tk.Name, tk.Name)
		}
		return nil
	case Radius:
		if tk.Name == "" {
			return fmt.Errorf("%s: Radius.Name is empty", path)
		}
		// "none" / "0" is a legitimate sharp-corner sentinel.
		if tk.Value == 0 && tk.Name != "none" && tk.Name != "0" {
			return fmt.Errorf("%s: Radius.Value is 0 (Name=%q) — use Radius{Name: \"none\"} for sharp corners explicitly", path, tk.Name)
		}
		return nil
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
			return fmt.Errorf("%s: ZIndex.Value is 0 (Name=%q) — use Name \"base\"/\"0\" if intentional", path, tk.Name)
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
	case CodeColor:
		// Optional group: an entirely-unset token is fine (component
		// CSS keeps its built-in fallback palette). Only a half-set
		// token is a configuration mistake.
		if tk.Value == "" && tk.Name != "" {
			return fmt.Errorf("%s: CodeColor.Value is empty (Name=%q) — leave the token fully zero to fall back, or give it a value", path, tk.Name)
		}
		if tk.Value != "" && tk.Name == "" {
			return fmt.Errorf("%s: CodeColor.Name is empty (Value=%q) — run AutoFillNames or set the Name", path, tk.Value)
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
			Primary:      Color{Name: "primary", Value: "#4F46E5"},
			PrimaryFg:    Color{Name: "primary-fg", Value: "#FFFFFF"},
			Secondary:    Color{Name: "secondary", Value: "#6B7280"},
			SecondaryFg:  Color{Name: "secondary-fg", Value: "#FFFFFF"},
			Background:   Color{Name: "background", Value: "#F9FAFB"},
			Surface:      Color{Name: "surface", Value: "#FFFFFF"},
			SurfaceSoft:  Color{Name: "surface-soft", Value: "#F4F4F5"},
			Text:         Color{Name: "text", Value: "#18181B"},
			TextMuted:    Color{Name: "text-muted", Value: "#52525B"},
			TextSubtle:   Color{Name: "text-subtle", Value: "#71717A"}, // 4.55:1 on surface — was #A1A1AA (2.56:1, fails AA)
			Border:       Color{Name: "border", Value: "#E4E4E7"},
			BorderStrong: Color{Name: "border-strong", Value: "#A1A1AA"},
			// Status tones are used two ways by framework/ui components:
			// as WHITE-TEXT FILLS (toasts, button--danger) and as LABEL
			// TEXT on their own 15%-tinted chips (Badge, Tag, StatCard
			// trend, ValidationSummary). The tint is the harder target —
			// the previous values (#DC2626 / #15803D / #A16207 / #2563EB)
			// hit 4.5:1 on white but only 3.7–4.2:1 on the tinted chips,
			// which axe flags on any light scheme. These shades clear
			// 4.6:1 on the chips and ≥6.4:1 with white fills.
			Danger:  Color{Name: "danger", Value: "#B91C1C"},  // 5.2:1 on its 15% chip — was #DC2626 (3.96:1)
			Success: Color{Name: "success", Value: "#166534"}, // 5.6:1 on its 15% chip — was #15803D (4.10:1)
			Warning: Color{Name: "warning", Value: "#854D0E"}, // 5.4:1 on its 15% chip — was #A16207 (4.03:1)
			Info:    Color{Name: "info", Value: "#1D4ED8"},    // 5.3:1 on its 15% chip — was #2563EB (4.23:1)
			Accent:  Color{Name: "accent", Value: "#7C3AED"},
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
			SM:   Radius{Name: "sm", Value: 4},
			MD:   Radius{Name: "md", Value: 8},
			LG:   Radius{Name: "lg", Value: 12},
			XL:   Radius{Name: "xl", Value: 16},
			Full: Radius{Name: "full", Value: 9999},
		},
		Fonts: FontSet{
			Body:    Font{Name: "body", Value: "'Inter', system-ui, sans-serif"},
			Heading: Font{Name: "heading", Value: "'Inter', system-ui, sans-serif"},
			Mono:    Font{Name: "mono", Value: "'JetBrains Mono', monospace"},
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
			SM:   Shadow{Name: "sm", Value: "0 1px 2px 0 rgba(0,0,0,0.05)"},
			MD:   Shadow{Name: "md", Value: "0 4px 6px -1px rgba(0,0,0,0.10), 0 2px 4px -1px rgba(0,0,0,0.06)"},
			LG:   Shadow{Name: "lg", Value: "0 10px 15px -3px rgba(0,0,0,0.10), 0 4px 6px -2px rgba(0,0,0,0.05)"},
			XL:   Shadow{Name: "xl", Value: "0 20px 25px -5px rgba(0,0,0,0.10), 0 10px 10px -5px rgba(0,0,0,0.04)"},
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
		Layout: LayoutSet{
			TouchTarget: Spacing{Name: "touch-target", Value: 44},
		},
		// Syntax-highlight palette (--tk-*). These are the values the
		// ui.CodeBlock CSS previously carried only as var() fallbacks —
		// promoted to theme slots so dark mode (Theme.DarkCode) and
		// re-skins can restyle code blocks. Tuned for the always-dark
		// default CodeSurface, so they hold in both page schemes. PN
		// chains to the code text color, matching the old `inherit`
		// fallback behavior.
		Code: CodeSet{
			KW:   CodeColor{Name: "kw", Value: "#C792EA"},
			FN:   CodeColor{Name: "fn", Value: "#82AAFF"},
			Str:  CodeColor{Name: "str", Value: "#C3E88D"},
			Num:  CodeColor{Name: "num", Value: "#F78C6C"},
			Com:  CodeColor{Name: "com", Value: "#676E95"},
			Type: CodeColor{Name: "type", Value: "#FFCB6B"},
			PN:   CodeColor{Name: "pn", Value: "var(--color-code-text)"},
		},
	}
}
