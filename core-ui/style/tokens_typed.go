package style

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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

// Stroke is a line-width token.
type Stroke struct {
	Name  string
	Value string
}

func (s Stroke) CSS() string    { return varRef("stroke", s.Name) }
func (s Stroke) String() string { return s.CSS() }

// LineHeight is a line-height token, emitted as --leading-<name>.
type LineHeight struct {
	Name  string
	Value string
}

func (l LineHeight) CSS() string    { return varRef("leading", l.Name) }
func (l LineHeight) String() string { return l.CSS() }

// LetterSpacing is a letter-spacing token, emitted as --tracking-<name>.
type LetterSpacing struct {
	Name  string
	Value string
}

func (l LetterSpacing) CSS() string    { return varRef("tracking", l.Name) }
func (l LetterSpacing) String() string { return l.CSS() }

// Opacity is an opacity token, emitted as --opacity-<name>.
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

// ZIndexValue is a z-index layer.
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

// FormattedValue returns the duration as a CSS <time> in milliseconds, at full
// precision ("150ms", "2.5ms", "0.5ms").
func (d Duration) FormattedValue() string {
	ms := d.Value / time.Millisecond
	ns := d.Value % time.Millisecond
	if ns == 0 {
		return strconv.FormatInt(int64(ms), 10) + "ms"
	}
	sign := ""
	if d.Value < 0 {
		sign, ms, ns = "-", -ms, -ns
	}
	frac := strings.TrimRight(fmt.Sprintf("%06d", int64(ns)), "0")
	return sign + strconv.FormatInt(int64(ms), 10) + "." + frac + "ms"
}

// Easing is a CSS timing-function token (e.g.
type Easing struct {
	Name  string
	Value string
}

func (e Easing) CSS() string    { return varRef("easing", e.Name) }
func (e Easing) String() string { return e.CSS() }

// FontSize is a typography-scale token.
type FontSize struct {
	Name  string
	Value string
}

func (f FontSize) CSS() string    { return varRef("text", f.Name) }
func (f FontSize) String() string { return f.CSS() }

// Size is a length token.
type Size struct {
	Name  string
	Value string
}

func (s Size) CSS() string    { return varRef("size", s.Name) }
func (s Size) String() string { return s.CSS() }

// FontWeight is a font-weight token, 1 to 1000, emitted as --font-
// weight-<name>.
type FontWeight struct {
	Name  string
	Value int
}

func (w FontWeight) CSS() string    { return varRef("font-weight", w.Name) }
func (w FontWeight) String() string { return w.CSS() }

// CodeColor is a syntax-highlight palette token.
type CodeColor struct {
	Name  string
	Value string
}

func (c CodeColor) CSS() string    { return varRef("tk", c.Name) }
func (c CodeColor) String() string { return c.CSS() }

func varRef(category, name string) string {
	return "var(--" + category + "-" + name + ")"
}
