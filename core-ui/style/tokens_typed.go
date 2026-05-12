package style

import (
	"fmt"
	"time"
)

// Typed token value types.
//
// Every theme token (color, spacing, shadow, …) is a struct carrying
// both a Name (used to derive the CSS custom property identifier)
// and a Value (the concrete value). Each type exposes a .CSS()
// method that emits `var(--<category>-<name>)` — a reference, never
// the literal value, so the CSS cascade can override the variable at
// any subtree (section-level theme override).
//
// The .Value field stays available for non-CSS contexts: dynamically
// constructing image-generation payloads, exporting tokens to JSON
// for design tools, computing derived colors at startup, etc.
//
// Each category has its own type — distinct from each other so the
// compiler catches accidentally passing a Color where Spacing was
// expected.

// Color holds a CSS color value (hex, rgb, hsl, named, oklch, etc.).
type Color struct {
	Name  string
	Value string
}

// CSS returns `var(--color-<name>)`.
func (c Color) CSS() string { return varRef("color", c.Name) }

// String returns CSS() for ease of use in fmt.Sprintf.
func (c Color) String() string { return c.CSS() }

// Spacing holds a pixel-valued spacing token.
type Spacing struct {
	Name  string
	Value int
}

func (s Spacing) CSS() string { return varRef("spacing", s.Name) }
func (s Spacing) String() string { return s.CSS() }

// Radius is a border-radius token. Pixel values, named.
type Radius struct {
	Name  string
	Value int
}

func (r Radius) CSS() string    { return varRef("radii", r.Name) }
func (r Radius) String() string { return r.CSS() }

// Font holds a font-family stack.
type Font struct {
	Name  string
	Value string
}

func (f Font) CSS() string    { return varRef("font", f.Name) }
func (f Font) String() string { return f.CSS() }

// Breakpoint is a viewport-width threshold (pixels).
type Breakpoint struct {
	Name  string
	Value int
}

func (b Breakpoint) CSS() string    { return varRef("breakpoint", b.Name) }
func (b Breakpoint) String() string { return b.CSS() }

// Shadow holds a CSS box-shadow expression (e.g. "0 4px 6px rgba(0,0,0,0.1)").
type Shadow struct {
	Name  string
	Value string
}

func (s Shadow) CSS() string    { return varRef("shadow", s.Name) }
func (s Shadow) String() string { return s.CSS() }

// ZIndexValue is a z-index layer. Use a distinct type name to avoid
// shadowing the ZIndex theme struct.
type ZIndexValue struct {
	Name  string
	Value int
}

func (z ZIndexValue) CSS() string    { return varRef("z", z.Name) }
func (z ZIndexValue) String() string { return z.CSS() }

// Duration is an animation/transition timing token.
type Duration struct {
	Name  string
	Value time.Duration
}

// CSSDuration formats a time.Duration as a CSS time value (e.g. "150ms").
func (d Duration) CSS() string    { return varRef("duration", d.Name) }
func (d Duration) String() string { return d.CSS() }

// FormattedValue returns the duration formatted for CSS (e.g. "150ms",
// "1s"). Used by CSSCustomProperties when emitting the :root block.
func (d Duration) FormattedValue() string {
	ms := d.Value.Milliseconds()
	if ms == 0 && d.Value > 0 {
		return fmt.Sprintf("%dus", d.Value.Microseconds())
	}
	return fmt.Sprintf("%dms", ms)
}

// FontSize is a typography-scale token. Value is the CSS size string
// (e.g. "1rem", "0.875rem", "clamp(...)" for fluid scales).
type FontSize struct {
	Name  string
	Value string
}

func (f FontSize) CSS() string    { return varRef("text", f.Name) }
func (f FontSize) String() string { return f.CSS() }

// varRef builds `var(--<category>-<name>)`. Centralized so the
// var-naming convention has exactly one definition.
func varRef(category, name string) string {
	return "var(--" + category + "-" + name + ")"
}
