package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TooltipPlacement selects the side the tooltip appears on; Left and
// Right are the reading direction's start and end.
type TooltipPlacement string

const (
	TooltipTop    TooltipPlacement = "" // default
	TooltipBottom TooltipPlacement = "bottom"
	TooltipLeft   TooltipPlacement = "left"
	TooltipRight  TooltipPlacement = "right"
)

// TooltipConfig configures a tooltip.
type TooltipConfig struct {
	// Text is the tooltip message. Required.
	Text string

	// Placement selects the side. Default top.
	Placement TooltipPlacement

	// ID is the pop's id, named by the trigger's aria-describedby; empty derives it from Text.
	ID string

	Class string

	// ExtraAttrs forwards attributes to the wrapper span; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// Tooltip wraps trigger with a role=tooltip pop that CSS shows on hover
// and focus, and points the trigger at it with aria-describedby.
func Tooltip(cfg TooltipConfig, trigger render.HTML) render.HTML {
	if cfg.Text == "" {
		panic("ui: Tooltip requires Text")
	}
	id := cfg.ID
	if id == "" {
		id = "tip-" + slug(cfg.Text)
	}

	cls := "fui-tooltip"
	placement := cfg.Placement
	if placement != TooltipTop {
		cls += " fui-tooltip--" + string(placement)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	triggerWithDescribedBy := injectTriggerDescribedBy(trigger, id)

	pop := html.Span(html.TextConfig{
		Class:      "fui-tooltip__pop",
		ID:         id,
		ExtraAttrs: html.Attrs{"role": "tooltip", "data-cui-internal": ""},
	}, render.Text(cfg.Text))

	return tooltipStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      cls,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, triggerWithDescribedBy, pop))
}

func injectTriggerDescribedBy(trigger render.HTML, id string) render.HTML {
	s := string(trigger)
	start := skipToFirstTag(s)
	if start < 0 {
		return trigger
	}
	end := findFirstTagClose(s[start:])
	if end < 0 {
		return trigger
	}
	end += start
	tag := s[start:end]
	if strings.Contains(tag, ` aria-describedby=`) {
		return trigger
	}
	insertAt := end
	if end > 0 && s[end-1] == '/' {
		insertAt = end - 1
	}
	attr := ` aria-describedby="` + string(render.Escape(id)) + `"`
	// safe-html: attr is assembled from render.Escape output only.
	return render.HTML(s[:insertAt] + attr + s[insertAt:])
}

func skipToFirstTag(s string) int {
	i := 0
	for i < len(s) {
		for i < len(s) {
			c := s[i]
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
				i++
				continue
			}
			break
		}
		if i+4 <= len(s) && s[i:i+4] == "<!--" {
			nc := strings.Index(s[i+4:], "-->")
			if nc < 0 {
				return -1
			}
			i = i + 4 + nc + 3
			continue
		}
		break
	}
	if i >= len(s) || s[i] != '<' {
		return -1
	}
	return i
}

func findFirstTagClose(s string) int {
	var quote byte
	for i := range len(s) {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '>':
			return i
		}
	}
	return -1
}
