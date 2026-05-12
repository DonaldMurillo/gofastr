package style

import (
	"fmt"
	"reflect"
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

	Colors      ColorSet
	Spacing     SpacingScale
	Radii       RadiusSet
	Fonts       FontSet
	Breakpoints BreakpointSet
	Shadows     ShadowSet
	ZIndex      ZIndexSet
	Durations   DurationSet
	Typography  FontSizeSet
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
type DurationSet struct {
	Fast, Normal, Slow Duration
}

// FontSizeSet — typography size scale.
type FontSizeSet struct {
	XS, SM, Base, LG, XL, XXL, XXXL FontSize
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
		return nil
	case Radius:
		if tk.Name == "" {
			return fmt.Errorf("%s: Radius.Name is empty", path)
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
		return nil
	case Duration:
		if tk.Name == "" {
			return fmt.Errorf("%s: Duration.Name is empty", path)
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
			TextSubtle:   Color{Name: "text-subtle", Value: "#A1A1AA"},
			Border:       Color{Name: "border", Value: "#E4E4E7"},
			BorderStrong: Color{Name: "border-strong", Value: "#A1A1AA"},
			Danger:       Color{Name: "danger", Value: "#DC2626"},
			Success:      Color{Name: "success", Value: "#16A34A"},
			Warning:      Color{Name: "warning", Value: "#CA8A04"},
			Info:         Color{Name: "info", Value: "#2563EB"},
			Accent:       Color{Name: "accent", Value: "#7C3AED"},
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
	}
}
