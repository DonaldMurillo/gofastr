package ui

import (
	"slices"
	"strings"
)

// chartTone is a mark's resolved color: a palette class or a raw fill.
type chartTone struct{ palette, fill string }

// seriesTone resolves a series Color the way BarChart resolves a bar's:
// palette token, registered status variant, plausible CSS color, else
// the i-th palette color.
func seriesTone(color string, i int) chartTone {
	switch {
	case slices.Contains(chartPalette, color):
		return chartTone{palette: color}
	case color == "":
	default:
		if c, ok := registeredStatusColor(color); ok {
			return chartTone{fill: c}
		}
		if plausibleCSSColor(color) {
			return chartTone{fill: color}
		}
	}
	return chartTone{palette: chartPalette[i%len(chartPalette)]}
}

// write closes the class attribute with the palette modifier, or with a
// fill attribute after it, and leaves the element open.
func (t chartTone) write(sb *strings.Builder, base string) {
	if t.palette != "" {
		sb.WriteString(` ` + base + `--` + t.palette + `" data-cui-internal=""`)
		return
	}
	sb.WriteString(`" data-cui-internal="" fill="`)
	sb.WriteString(escapeXML(t.fill))
	sb.WriteString(`"`)
}

var chartPalette = []string{"primary", "info", "success", "warning", "danger"}
