package ui

import (
	"context"
	"hash/fnv"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── DrawerBar ──────────────────────────────────────────────────────
//
// The bar across the top of an intercepted drawer (app.InterceptFrom):
// a close control, the layer's path in mono, and trailing actions. It
// sticks to the top of the layer while the layer scrolls and bleeds to
// the layer's edges. A screen draws it only when
// app.OverlayFromContext reports an overlay; the canonical full page
// has breadcrumbs instead.
//
// Close carries data-cui-intercept-close, so it is the same history
// move as Escape and the backdrop, and a form with unsaved edits asks
// first.

// DrawerBarConfig configures the bar.
type DrawerBarConfig struct {
	// Path is the layer's own URL path, shown in mono and truncated
	// from the end when it does not fit. Required.
	Path string
	// CopyURL, when set, adds an icon button that copies it: the
	// layer's absolute address, so a reader can share the record.
	CopyURL string
	// Actions trail the bar after the copy button.
	Actions []render.HTML
	// Ctx resolves the close and copy labels. Nil means English.
	Ctx context.Context
	// ExtraAttrs lands on the bar's root; class, id and data-cui-*
	// are dropped.
	ExtraAttrs html.Attrs
}

// DrawerBar renders the bar.
func DrawerBar(cfg DrawerBarConfig) render.HTML {
	if cfg.Path == "" {
		panic("ui: DrawerBar requires Path")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	kids := []render.HTML{
		Button(ButtonConfig{
			Label:      i18nui.T(ctx, i18nui.KeyDialogClose),
			Icon:       "close",
			IconOnly:   true,
			Variant:    ButtonGhost,
			Size:       ButtonSizeSmall,
			ExtraAttrs: html.Attrs{"data-cui-intercept-close": ""},
		}),
		html.Code(html.TextConfig{Class: "fui-drawer-bar__path", ExtraAttrs: html.Attrs{"title": cfg.Path}}, render.Text(cfg.Path)),
	}
	if cfg.CopyURL != "" {
		// One id per path: a stack never holds two layers on one path
		// (a link to the top layer's own path re-renders it), so the
		// copy never reads a lower layer's address.
		h := fnv.New32a()
		h.Write([]byte(cfg.Path))
		id := "fui-drawer-url-" + strconv.FormatUint(uint64(h.Sum32()), 36)
		kids = append(kids,
			html.Span(html.TextConfig{ID: id, Class: "fui-visually-hidden"}, render.Text(cfg.CopyURL)),
			CopyButton(CopyButtonConfig{
				Target:      id,
				IconOnly:    true,
				Icon:        "link",
				AriaLabel:   i18nui.T(ctx, i18nui.KeyCopyLink),
				ToastOnCopy: true,
				ToastTitle:  i18nui.T(ctx, i18nui.KeyCopyCopied),
				Ctx:         ctx,
			}),
		)
	}
	// The bar's own controls are internal; Actions stay the caller's.
	kids = append([]render.HTML{headless.Own(render.Join(kids...))}, cfg.Actions...)
	return drawerBarStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-drawer-bar", ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)}, kids...))
}

var drawerBarStyle = registry.RegisterStyle("ui-drawer-bar", drawerBarCSS)

func drawerBarCSS(_ style.Theme) string {
	// --cui-intercept-pad is the layer's inset, set by
	// app.InterceptOverlayCSS on every layer; outside a layer it is
	// unset and the bar sits flush where it is placed. A sticky box
	// stops at the scroller's padding edge, so the bar's offset is the
	// inset taken back: it sticks at the layer's top edge.
	return `[data-cui-comp="ui-drawer-bar"] {
  position: sticky;
  inset-block-start: calc(-1 * var(--cui-intercept-pad, 0px));
  z-index: 1;
  display: flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  margin: calc(-1 * var(--cui-intercept-pad, 0px)) calc(-1 * var(--cui-intercept-pad, 0px)) var(--spacing-lg, 16px);
  padding: var(--spacing-sm, 4px) calc(var(--cui-intercept-pad, 0px) - var(--spacing-sm, 4px));
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
  background: var(--color-surface, #fff);
}
[data-cui-comp="ui-drawer-bar"] .fui-drawer-bar__path {
  flex: 1 1 auto;
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding-inline: var(--spacing-sm, 4px);
  background: none;
  color: var(--color-text-muted, #52525b);
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 0.75rem);
}
`
}
