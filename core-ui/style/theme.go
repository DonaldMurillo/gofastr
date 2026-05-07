package style

// Theme defines the complete visual design system.
type Theme struct {
	Name        string
	Colors      Colors
	Spacing     Spacing
	Radii       Radii
	Fonts       Fonts
	Breakpoints Breakpoints
	Components  ComponentStyles // named component style definitions
}

// Colors maps color names to CSS color values.
type Colors map[string]string // e.g., "primary": "#4F46E5"

// Spacing maps spacing token names to pixel values.
type Spacing map[string]int // e.g., "md": 8

// Radii maps border-radius token names to pixel values.
type Radii map[string]int // e.g., "lg": 12

// Fonts maps font role names to CSS font-family values.
type Fonts map[string]string // e.g., "body": "'Inter', sans-serif"

// Breakpoints maps breakpoint names to pixel values.
type Breakpoints map[string]int // e.g., "md": 768

// ComponentStyles maps component style names to their style definitions.
type ComponentStyles map[string]StyleDef

// StyleDef defines CSS properties for a named component style.
type StyleDef map[string]string // e.g., "padding": "{spacing.md} {spacing.lg}"

// DefaultTheme returns a sensible default theme with a complete set of design tokens.
func DefaultTheme() Theme {
	return Theme{
		Name: "default",
		Colors: Colors{
			"primary":    "#4F46E5",
			"secondary":  "#6B7280",
			"danger":     "#EF4444",
			"success":    "#10B981",
			"warning":    "#F59E0B",
			"info":       "#3B82F6",
			"surface":    "#FFFFFF",
			"background": "#F9FAFB",
			"text":       "#1F2937",
			"text-muted": "#6B7280",
			"border":     "#E5E7EB",
		},
		Spacing: Spacing{
			"xs":  2,
			"sm":  4,
			"md":  8,
			"lg":  16,
			"xl":  24,
			"2xl": 32,
			"3xl": 48,
		},
		Radii: Radii{
			"none": 0,
			"sm":   4,
			"md":   8,
			"lg":   12,
			"xl":   16,
			"full": 9999,
		},
		Fonts: Fonts{
			"body":    "'Inter', system-ui, sans-serif",
			"heading": "'Inter', system-ui, sans-serif",
			"mono":    "'JetBrains Mono', monospace",
		},
		Breakpoints: Breakpoints{
			"sm":  640,
			"md":  768,
			"lg":  1024,
			"xl":  1280,
			"2xl": 1536,
		},
		Components: ComponentStyles{},
	}
}

// MergeThemes overlays custom on top of base, returning a new Theme.
// For map fields, individual entries are merged (custom entries override base).
// Scalar fields (Name) are taken from custom if non-empty.
func MergeThemes(base, custom Theme) Theme {
	result := Theme{
		Name:        base.Name,
		Colors:      mergeColors(base.Colors, custom.Colors),
		Spacing:     mergeSpacing(base.Spacing, custom.Spacing),
		Radii:       mergeRadii(base.Radii, custom.Radii),
		Fonts:       mergeFonts(base.Fonts, custom.Fonts),
		Breakpoints: mergeBreakpoints(base.Breakpoints, custom.Breakpoints),
		Components:  mergeComponentStyles(base.Components, custom.Components),
	}
	if custom.Name != "" {
		result.Name = custom.Name
	}
	return result
}

func mergeColors(base, custom Colors) Colors {
	result := make(Colors, len(base)+len(custom))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}

func mergeSpacing(base, custom Spacing) Spacing {
	result := make(Spacing, len(base)+len(custom))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}

func mergeRadii(base, custom Radii) Radii {
	result := make(Radii, len(base)+len(custom))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}

func mergeFonts(base, custom Fonts) Fonts {
	result := make(Fonts, len(base)+len(custom))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}

func mergeBreakpoints(base, custom Breakpoints) Breakpoints {
	result := make(Breakpoints, len(base)+len(custom))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}

func mergeComponentStyles(base, custom ComponentStyles) ComponentStyles {
	result := make(ComponentStyles, len(base)+len(custom))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range custom {
		result[k] = v
	}
	return result
}
