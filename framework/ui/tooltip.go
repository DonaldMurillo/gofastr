package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"strings"
)

// ─── Tooltip ────────────────────────────────────────────────────────
//
// A CSS-only hover/focus tooltip. The visible pop element is always
// present in the DOM and toggled by `:hover` / `:focus-visible` /
// `:focus-within` rules on the wrapper, no JavaScript required, no
// runtime callouts, no flash-on-mount.
//
// The wrapper carries data-fui-comp="ui-tooltip" so the stylesheet
// loads lazily on first appearance. The popped element is wired via
// aria-describedby so screen readers announce the tooltip alongside
// the trigger.

// TooltipPlacement selects the side the tooltip appears on.
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

	// ID is the tooltip's id; the trigger's aria-describedby points
	// to it. When empty, a stable id is derived from the trigger's
	// content position.
	ID string

	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the tooltip's root
	// wrapper <span>. Keys the component owns are dropped: class
	// and id (use Class / ID), data-fui-*.
	ExtraAttrs html.Attrs
}

// Tooltip wraps the given trigger HTML and appends a hidden tooltip
// pop. The trigger is unwrapped. Tooltip only adds a containing
// span + the pop element, so inline buttons and links stay inline.
//
// Use on icon-only buttons, truncated labels, or anywhere extra
// context is useful without occupying layout space.
func Tooltip(cfg TooltipConfig, trigger render.HTML) render.HTML {
	if cfg.Text == "" {
		panic("ui: Tooltip requires Text")
	}
	id := cfg.ID
	if id == "" {
		// Derive a content-stable id so SSR and runtime agree.
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

	// The trigger receives aria-describedby: the caller's element is
	// pre-built markup, so the attribute is spliced into its first
	// open tag. This is Tooltip's own local splice — FormField's old
	// injectAttrs was deleted with its composition hack; a tooltip
	// trigger has no builder seam to hand wiring to (yet).
	triggerWithDescribedBy := injectTriggerDescribedBy(trigger, id)

	pop := html.Span(html.TextConfig{
		Class:      "fui-tooltip__pop",
		ID:         id,
		ExtraAttrs: html.Attrs{"role": "tooltip"},
	}, render.Text(cfg.Text))

	return tooltipStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      cls,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, triggerWithDescribedBy, pop))
}

// injectTriggerDescribedBy splices ` aria-describedby="<id>"` into the
// first open tag of the caller's trigger element. Idempotent: a trigger
// whose FIRST OPEN TAG already carries the attribute is returned
// unchanged — the check is scoped to that tag, because the trigger is a
// subtree (a button wrapping an icon, an input wrapping nothing) and a
// DESCENDANT carrying aria-describedby is its own description, not
// evidence the trigger root is wired. Local to Tooltip: its trigger is
// caller-built markup with no builder seam, so the relationship is
// spliced rather than handed down (see FormField for the seam-based
// alternative).
func injectTriggerDescribedBy(trigger render.HTML, id string) render.HTML {
	s := string(trigger)
	// Find the real open tag, skipping leading whitespace and HTML
	// comments. The splice target is the `>` that closes that tag,
	// respecting attribute quotes (so `>` inside `title="a > b"` doesn't
	// terminate the tag prematurely).
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

// skipToFirstTag returns the index of the first byte of the outermost
// real open tag, skipping whitespace and HTML comments (a comment's
// `-->` is not a tag close). Returns -1 when no open tag is found.
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

// findFirstTagClose returns the index of the first `>` that closes the
// open tag starting at offset 0 of s, respecting attribute quotes.
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
