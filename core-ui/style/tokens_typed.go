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
// method that emits `var(--<category>-<name>)`, a reference, never
// the literal value, so the CSS cascade can override the variable at
// any subtree (section-level theme override).
//
// The .Value field stays available for non-CSS contexts: dynamically
// constructing image-generation payloads, exporting tokens to JSON
// for design tools, computing derived colors at startup, etc.
//
// Each category has its own type, distinct from each other so the
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

func (s Spacing) CSS() string    { return varRef("spacing", s.Name) }
func (s Spacing) String() string { return s.CSS() }

// Radius is a border-radius token. Pixel values, named.
type Radius struct {
	Name  string
	Value int
}

func (r Radius) CSS() string    { return varRef("radii", r.Name) }
func (r Radius) String() string { return r.CSS() }

// Stroke is a line-width token: a border, a divider, a focus outline
// and its offset. Value is a non-negative CSS length ("1px", "0.125rem")
// or "0", which is a real width (a borderless theme). The set is
// optional: a token left fully zero emits the default theme's width, so
// a theme written before strokes existed keeps its borders and every
// var(--stroke-*) reader resolves.
type Stroke struct {
	Name  string
	Value string
}

func (s Stroke) CSS() string    { return varRef("stroke", s.Name) }
func (s Stroke) String() string { return s.CSS() }

// LineHeight is a line-height token, emitted as --leading-<name>. Value
// is a unitless multiplier ("1.5") or a px/rem/em length. Optional like
// Stroke: a token left fully zero is not emitted and the kit's var()
// fallback draws the default.
type LineHeight struct {
	Name  string
	Value string
}

func (l LineHeight) CSS() string    { return varRef("leading", l.Name) }
func (l LineHeight) String() string { return l.CSS() }

// LetterSpacing is a letter-spacing token, emitted as --tracking-<name>.
// Value is "0" or a signed px/rem/em length ("-0.02em"). Optional like
// Stroke.
type LetterSpacing struct {
	Name  string
	Value string
}

func (l LetterSpacing) CSS() string    { return varRef("tracking", l.Name) }
func (l LetterSpacing) String() string { return l.CSS() }

// Opacity is an opacity token, emitted as --opacity-<name>. Value is a
// number from 0 to 1. Optional like Stroke.
type Opacity struct {
	Name  string
	Value string
}

func (o Opacity) CSS() string    { return varRef("opacity", o.Name) }
func (o Opacity) String() string { return o.CSS() }

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

// Easing is a CSS timing-function token (e.g. "cubic-bezier(...)",
// "linear", "ease-out"). Used by widget chrome to keep animation
// curves theme-driven rather than hard-coded in stylesheets.
type Easing struct {
	Name  string
	Value string
}

func (e Easing) CSS() string    { return varRef("easing", e.Name) }
func (e Easing) String() string { return e.CSS() }

// FontSize is a typography-scale token. Value is the CSS size string
// (e.g. "1rem", "0.875rem", "clamp(...)" for fluid scales).
type FontSize struct {
	Name  string
	Value string
}

func (f FontSize) CSS() string    { return varRef("text", f.Name) }
func (f FontSize) String() string { return f.CSS() }

// Size is a length token: a page column, a bar height, a measure. Value
// is a CSS length ("66rem", "56px") or a calc()/clamp()/min()/max()
// expression over lengths, emitted as --size-<name>. Spacing stays the
// pixel scale for gaps and padding; Size is for the dimensions a layout
// is built around.
type Size struct {
	Name  string
	Value string
}

func (s Size) CSS() string    { return varRef("size", s.Name) }
func (s Size) String() string { return s.CSS() }

// FontWeight is a font-weight token, 1 to 1000, emitted as
// --font-weight-<name>.
type FontWeight struct {
	Name  string
	Value int
}

func (w FontWeight) CSS() string    { return varRef("font-weight", w.Name) }
func (w FontWeight) String() string { return w.CSS() }

// CodeColor is a syntax-highlight palette token. It emits
// `var(--tk-<name>)`, the --tk-* variables the framework
// highlighter's .tk-* spans (ui.CodeBlock, markdown code fences)
// read. Unlike the other token groups the Code group is OPTIONAL:
// a zero CodeColor is skipped by validation and never emitted, and
// the component CSS falls back to its built-in palette, so themes
// predating the group keep working unchanged.
type CodeColor struct {
	Name  string
	Value string
}

func (c CodeColor) CSS() string    { return varRef("tk", c.Name) }
func (c CodeColor) String() string { return c.CSS() }

// varRef builds `var(--<category>-<name>)`. Centralized so the
// var-naming convention has exactly one definition.
func varRef(category, name string) string {
	return "var(--" + category + "-" + name + ")"
}
