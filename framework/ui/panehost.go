package ui

import (
	"context"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// PaneHostConfig configures a PaneHost.
type PaneHostConfig struct {
	// Primary is the always-visible main pane; PaneHost panics when it is empty.
	Primary render.HTML
	// Secondary is the first optional side pane, not rendered when empty.
	Secondary render.HTML
	// Tertiary is the second optional side pane.
	Tertiary render.HTML

	// SecondaryOpen and TertiaryOpen set the first-paint open state; a closed pane renders hidden.
	SecondaryOpen bool
	TertiaryOpen  bool

	// SecondaryLabel and TertiaryLabel name each side pane's region, "Secondary" and
	// "Tertiary" in Ctx's language when empty.
	SecondaryLabel string
	TertiaryLabel  string

	// Ctx is the render context the default pane names are said in; nil is English.
	Ctx context.Context

	// DeepLinkParam names the query parameter that records the open pane and the key
	// its trigger declares with interactive.PaneKey (?pane=secondary:ticket-42); empty
	// leaves the URL alone. Read it with PaneDeepLink to paint the pane open on the server.
	DeepLinkParam string

	ID    string
	Class string

	// ExtraAttrs is forwarded to the root without the keys the component owns.
	ExtraAttrs html.Attrs
}

// PaneHost renders a primary pane plus one or two openable side panes.
func PaneHost(cfg PaneHostConfig) render.HTML {
	if cfg.Primary == "" {
		panic("ui: PaneHost requires Primary")
	}

	secondaryOpen := cfg.Secondary != "" && cfg.SecondaryOpen
	tertiaryOpen := cfg.Tertiary != "" && cfg.TertiaryOpen

	rootCls := "fui-pane-host"
	if secondaryOpen {
		rootCls += " fui-pane-host--secondary-open"
	}
	if tertiaryOpen {
		rootCls += " fui-pane-host--tertiary-open"
	}
	if cfg.Class != "" {
		rootCls += " " + cfg.Class
	}

	classes := headless.Classes{
		headless.PartRoot: rootCls,
		headless.PartPane: "fui-pane-host__pane",
	}
	classes[headless.Part("pane--secondary")] = "fui-pane-host__pane--secondary"
	classes[headless.Part("pane--primary")] = "fui-pane-host__pane--primary"
	classes[headless.Part("pane--tertiary")] = "fui-pane-host__pane--tertiary"

	out := headless.PaneHost(headless.PaneHostProps{
		Primary:        cfg.Primary,
		Secondary:      cfg.Secondary,
		Tertiary:       cfg.Tertiary,
		SecondaryOpen:  cfg.SecondaryOpen,
		TertiaryOpen:   cfg.TertiaryOpen,
		SecondaryLabel: cfg.SecondaryLabel,
		TertiaryLabel:  cfg.TertiaryLabel,
		DeepLinkParam:  cfg.DeepLinkParam,
		ID:             cfg.ID,
		ExtraAttrs:     headless.Safe(cfg.ExtraAttrs, "class"),
		Strings:        StringsFor(cfg.Ctx),
	}, classes)
	return paneHostStyle.WrapHTML(out)
}

// PaneDeepLink reads a PaneHost deep link, <slot> or <slot>:<key>, from q. ok is
// false unless slot is secondary or tertiary. key is untrusted: look it up, never
// reflect it.
func PaneDeepLink(q url.Values, param string) (slot, key string, ok bool) {
	if param == "" || q == nil {
		return "", "", false
	}
	raw := q.Get(param)
	if raw == "" {
		return "", "", false
	}
	slot, key, _ = strings.Cut(raw, ":")
	if slot != "secondary" && slot != "tertiary" {
		return "", "", false
	}
	return slot, key, true
}

var paneHostStyle = registry.RegisterStyle("ui-pane-host", paneHostCSS)

func paneHostCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-pane-host"] {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--fui-pane-host-gap, var(--spacing-lg, 16px));
  align-items: start;
  position: relative;
}
[data-cui-comp="ui-pane-host"] .fui-pane-host__pane { min-width: 0; }
/* hidden must win over any pane display rule so a closed pane never
   claims a grid track or paints at first paint. */
[data-cui-comp="ui-pane-host"] [data-hui-pane][hidden] { display: none; }

/* Column count keys off the open-state HOOK the runtime module
   maintains ([data-hui-pane-open], a space-separated list, matched
   token-wise with ~=), so a pane opened in the browser changes the
   columns; the open modifier classes ride along as the first-paint
   spelling. Never inline style (strict CSP, Hard Rule 9b). */
[data-cui-comp="ui-pane-host"].fui-pane-host--secondary-open,
[data-cui-comp="ui-pane-host"][data-hui-pane-open~="secondary"] {
  grid-template-columns: minmax(0, 1fr) var(--fui-pane-host-secondary-w, 360px);
}
[data-cui-comp="ui-pane-host"].fui-pane-host--tertiary-open:not(.fui-pane-host--secondary-open),
[data-cui-comp="ui-pane-host"][data-hui-pane-open~="tertiary"]:not([data-hui-pane-open~="secondary"]) {
  grid-template-columns: minmax(0, 1fr) var(--fui-pane-host-tertiary-w, 300px);
}
[data-cui-comp="ui-pane-host"].fui-pane-host--secondary-open.fui-pane-host--tertiary-open,
[data-cui-comp="ui-pane-host"][data-hui-pane-open~="secondary"][data-hui-pane-open~="tertiary"] {
  grid-template-columns: minmax(0, 1fr) var(--fui-pane-host-secondary-w, 360px) var(--fui-pane-host-tertiary-w, 300px);
}

[data-cui-comp="ui-pane-host"] .fui-pane-host__pane--secondary,
[data-cui-comp="ui-pane-host"] .fui-pane-host__pane--tertiary {
  background: var(--color-surface, transparent);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  padding: var(--spacing-md, 8px);
}

/* Narrow viewport: the grid collapses to a single column. An open side
   pane renders as a fixed overlay drawer when the module sets
   data-hui-pane-mode="overlay" on the host (it does so once
   matchMedia(max-width: 768px) matches AND a pane is open). The
   breakpoint literal here MUST match the MQ in
   framework/headless/panehost.js. */
@media (max-width: 768px) {
  [data-cui-comp="ui-pane-host"],
  [data-cui-comp="ui-pane-host"].fui-pane-host--secondary-open,
  [data-cui-comp="ui-pane-host"].fui-pane-host--tertiary-open,
  [data-cui-comp="ui-pane-host"].fui-pane-host--secondary-open.fui-pane-host--tertiary-open,
  [data-cui-comp="ui-pane-host"][data-hui-pane-open~="secondary"],
  [data-cui-comp="ui-pane-host"][data-hui-pane-open~="tertiary"],
  [data-cui-comp="ui-pane-host"][data-hui-pane-open~="secondary"][data-hui-pane-open~="tertiary"] {
    grid-template-columns: minmax(0, 1fr);
  }
}

/* Drawer chrome while an open pane is in overlay mode. */
[data-hui-pane-mode="overlay"] [data-hui-pane="secondary"]:not([hidden]),
[data-hui-pane-mode="overlay"] [data-hui-pane="tertiary"]:not([hidden]) {
  position: fixed;
  inset-block: 0;
  inset-inline-end: 0;
  block-size: 100dvh;
  inline-size: min(90vw, var(--fui-pane-host-drawer-w, 420px));
  z-index: var(--z-modal, 300);
  border-radius: 0;
  border-inline-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  box-shadow: var(--shadow-md, 0 10px 30px rgba(0, 0, 0, 0.18));
  overflow: auto;
  /* The drawer scrolls on its own: sticky parts pin to its top. */
  --ui-sticky-top: 0px;
}

/* Backdrop scrim while a drawer is open. The host's ::before covers the
   viewport; a click on it lands on the host itself, which the module
   treats as a dismiss (backdrop-click closes the topmost pane). It sits
   one step BELOW the drawer's modal tier: on the framework token scale
   --z-popover (400) is ABOVE --z-modal (300), so the scrim must key off
   --z-modal, not --z-popover, or it would paint over the drawer and eat
   every click inside it. */
[data-hui-pane-mode="overlay"]::before {
  content: "";
  position: fixed;
  inset: 0;
  background: var(--fui-pane-host-scrim, rgba(10, 9, 15, 0.42));
  z-index: calc(var(--z-modal, 300) - 1);
}`
}
