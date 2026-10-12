package ui

// ─── ShortID ────────────────────────────────────────────────────────
//
// A long identifier (a UUID, a job id, a hash) in a table cell: its
// first characters in monospace, then a quiet copy button that copies
// the whole value. The full value is the shortened text's title on
// hover, and screen readers hear it once, from a visually hidden copy
// that is also what the button copies.

import (
	"context"
	"hash/fnv"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// shortIDLength is how many characters a ShortID shows by default.
const shortIDLength = 8

// ShortIDConfig configures a ShortID.
type ShortIDConfig struct {
	// Value is the full identifier. Required.
	Value string
	// Length is how many leading characters show; a longer Value is
	// cut there and ends in an ellipsis. Default 8.
	Length int
	// Ctx resolves the copy button's label. When nil, English applies.
	Ctx   context.Context
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the root span. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// style and data-cui-*.
	ExtraAttrs html.Attrs
}

// ShortID renders the identifier and its copy button. It panics on an
// empty Value: there is nothing to show or copy.
func ShortID(cfg ShortIDConfig) render.HTML {
	if cfg.Value == "" {
		panic("ui: ShortID requires Value")
	}
	n := cfg.Length
	if n <= 0 {
		n = shortIDLength
	}
	// One id per value: the same value twice on a page holds the same
	// text, so either copy target copies it.
	h := fnv.New32a()
	h.Write([]byte(cfg.Value))
	target := "fui-short-id-" + strconv.FormatUint(uint64(h.Sum32()), 36)

	var shown []render.HTML
	if runes := []rune(cfg.Value); len(runes) > n {
		shown = []render.HTML{
			headless.Own(html.Code(html.TextConfig{
				Class:      "fui-short-id__code",
				ExtraAttrs: html.Attrs{"title": cfg.Value, "aria-hidden": "true"},
			}, render.Text(string(runes[:n])+"…"))),
			headless.Own(html.Span(html.TextConfig{ID: target, Class: "fui-visually-hidden"}, render.Text(cfg.Value))),
		}
	} else {
		shown = []render.HTML{
			headless.Own(html.Code(html.TextConfig{ID: target, Class: "fui-short-id__code"}, render.Text(cfg.Value))),
		}
	}
	copyBtn := headless.Own(CopyButton(CopyButtonConfig{
		Target: target, IconOnly: true, Icon: "copy", Inline: true, Ctx: cfg.Ctx,
	}))

	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	cls := "fui-short-id"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs["class"] = cls
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return shortIDStyle.WrapHTML(render.Tag("span", attrs, append(shown, copyBtn)...))
}

var shortIDStyle = registry.RegisterStyle("ui-short-id", shortIDCSS)

func shortIDCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-short-id"] {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  white-space: nowrap;
  min-inline-size: 0;
}
/* In a box narrower than the id (a phone row's cell) the code gives
   way and the copy button stays whole. */
[data-cui-comp="ui-short-id"] .fui-short-id__code {
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: var(--text-sm, 0.875rem);
}
:where([data-cui-comp="ui-short-id"] .fui-copy-btn-wrap) {
  flex: none;
}
[data-cui-comp="ui-short-id"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
`
}
