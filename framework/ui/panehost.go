package ui

// PaneHost is a layout shell with an always-visible primary pane and one
// or two openable side panes (secondary / tertiary). It owns the pane
// LIFECYCLE: show/hide, focus handoff on open, focus restore on close,
// and a responsive collapse where, below 768px, an open side pane
// becomes a fixed overlay drawer (backdrop scrim + focus trap + scroll
// lock + ESC-to-close) instead of an inline grid column.
//
// It does NOT fetch pane content. Loading a link's content into a pane
// uses the EXISTING rails: a trigger carries data-fui-rpc + a
// data-fui-rpc-signal that broadcasts into a data-fui-signal +
// data-fui-signal-mode="html" region inside the pane. Pane open/close
// is in-page state, never a URL route (Hard Rule 1).
//
// A host can still round-trip that state through the URL: set
// DeepLinkParam and the runtime records the open pane in a query
// parameter, so refresh, share, and Back reproduce it. That is a query
// parameter describing in-page state, not a route, the same shape
// widget deep links use for modals.
//
// Shape (mirrors DocLayout, a display:grid whose column count CSS keys
// off the host's open-state hook and the open modifier classes, so no
// inline style is emitted and CSP stays strict):
//
//	list := ui.PaneHost(ui.PaneHostConfig{
//	    Primary:   customerList,
//	    Secondary: detailRegion,   // optional
//	    Tertiary:  inspectorRegion, // optional
//	    SecondaryOpen:  false,      // SSR first-paint state (Hard Rule 6)
//	    SecondaryLabel: "Details",  // labels the role="region"
//	})
//
// See framework/docs/content/pane-host.md for the trigger attributes
// (data-hui-pane-open-control / -close / -swap) and the drawer collapse.

import (
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
	// Primary is the always-visible main pane. Required: PaneHost
	// panics when it is empty (mirrors DataTable/DocLayout required
	// slots).
	Primary render.HTML
	// Secondary is the first optional side pane. When empty, no
	// secondary pane is rendered.
	Secondary render.HTML
	// Tertiary is the second optional side pane.
	Tertiary render.HTML

	// SecondaryOpen / TertiaryOpen set the SSR initial open state so
	// the first paint matches server state (Hard Rule 6), e.g. a
	// detail route that should render with the pane already shown. A
	// closed optional pane renders with hidden; the runtime reveals it
	// on open so there is no flash.
	SecondaryOpen bool
	TertiaryOpen  bool

	// SecondaryLabel / TertiaryLabel label each side pane's
	// role="region" via aria-label. Empty falls back to "Secondary" /
	// "Tertiary".
	SecondaryLabel string
	TertiaryLabel  string

	// DeepLinkParam opts this host into URL round-tripping, naming the
	// query parameter that carries pane state, e.g. "pane" for
	// `?pane=secondary:ticket-42`. Opening a pane whose trigger declares
	// a key writes that parameter, closing strips it, and Back moves
	// between those states. Empty (the default) leaves the URL alone.
	//
	// Pane open/close remains in-page state, not a route (Hard Rule 1):
	// the parameter records which pane is showing so a refresh or a
	// shared link reproduces it, exactly as widget deep links do for
	// modals. The server still decides first paint. Read the parameter
	// with PaneDeepLink and set SecondaryOpen/TertiaryOpen plus the
	// pane's content from it. Without that, a shared link renders the
	// URL's pane closed and the runtime opens it after hydration.
	//
	// Triggers declare their key with interactive.PaneKey; a trigger
	// with no key opens the pane without touching the URL.
	DeepLinkParam string

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the host's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID) and data-fui-* (the pane-host marker and deep-link wiring).
	ExtraAttrs html.Attrs
}

// PaneHost renders a primary pane plus one or two openable side panes
// through headless.PaneHost with the fui-pane-host class map (the
// sheet name "ui-pane-host" and marker stay). The root carries the
// open state as data-hui-pane-open (the list the runtime module
// maintains and the sheet's column rules key off, so a client-side
// open changes the columns); the open modifier classes ride along for
// first paint and any caller that reads them.
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
	// Each pane's slot modifier rides beside the shared class, resolved
	// by headless.PaneHost through Classes.Variant(PartPane, slot).
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
	}, classes)
	return paneHostStyle.WrapHTML(out)
}

// PaneDeepLink reads a PaneHost deep link out of a request's query.
//
// The value is `<slot>` or `<slot>:<key>`, where slot is "secondary" or
// "tertiary" and key identifies what the pane is showing. It is the
// server half of PaneHostConfig.DeepLinkParam, and it parses exactly
// what the runtime writes. Keep the two in step:
//
//	q := appui.QueryFromContext(ctx)
//	slot, key, ok := ui.PaneDeepLink(q, "pane")
//	detail := emptyState
//	if ok && slot == "secondary" {
//	    if t, found := ticketByID(key); found {
//	        detail = renderTicket(t)   // first paint already shows it
//	    }
//	}
//
// ok is false when the parameter is missing or names something that is
// not an openable side pane, so an edited URL degrades to the ordinary
// closed-pane render rather than an error. Callers must still treat key
// as untrusted input and look it up rather than reflecting it.
//
// Only the first colon splits, so keys may contain colons.
func PaneDeepLink(q url.Values, param string) (slot, key string, ok bool) {
	if param == "" || q == nil {
		return "", "", false
	}
	raw := q.Get(param)
	if raw == "" {
		return "", "", false
	}
	slot, key, _ = strings.Cut(raw, ":")
	// "primary" is always visible and never openable, so it is not a
	// valid deep-link target either.
	if slot != "secondary" && slot != "tertiary" {
		return "", "", false
	}
	return slot, key, true
}

var paneHostStyle = registry.RegisterStyle("ui-pane-host", paneHostCSS)

func paneHostCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-pane-host"] {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--fui-pane-host-gap, var(--spacing-lg, 16px));
  align-items: start;
  position: relative;
}
[data-fui-comp="ui-pane-host"] .fui-pane-host__pane { min-width: 0; }
/* hidden must win over any pane display rule so a closed pane never
   claims a grid track or paints at first paint. */
[data-fui-comp="ui-pane-host"] [data-hui-pane][hidden] { display: none; }

/* Column count keys off the open-state HOOK the runtime module
   maintains ([data-hui-pane-open], a space-separated list, matched
   token-wise with ~=), so a pane opened in the browser changes the
   columns; the open modifier classes ride along as the first-paint
   spelling. Never inline style (strict CSP, Hard Rule 9b). */
[data-fui-comp="ui-pane-host"].fui-pane-host--secondary-open,
[data-fui-comp="ui-pane-host"][data-hui-pane-open~="secondary"] {
  grid-template-columns: minmax(0, 1fr) var(--fui-pane-host-secondary-w, 360px);
}
[data-fui-comp="ui-pane-host"].fui-pane-host--tertiary-open:not(.fui-pane-host--secondary-open),
[data-fui-comp="ui-pane-host"][data-hui-pane-open~="tertiary"]:not([data-hui-pane-open~="secondary"]) {
  grid-template-columns: minmax(0, 1fr) var(--fui-pane-host-tertiary-w, 300px);
}
[data-fui-comp="ui-pane-host"].fui-pane-host--secondary-open.fui-pane-host--tertiary-open,
[data-fui-comp="ui-pane-host"][data-hui-pane-open~="secondary"][data-hui-pane-open~="tertiary"] {
  grid-template-columns: minmax(0, 1fr) var(--fui-pane-host-secondary-w, 360px) var(--fui-pane-host-tertiary-w, 300px);
}

[data-fui-comp="ui-pane-host"] .fui-pane-host__pane--secondary,
[data-fui-comp="ui-pane-host"] .fui-pane-host__pane--tertiary {
  background: var(--color-surface, transparent);
  border: 1px solid var(--color-border, rgba(0, 0, 0, 0.10));
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
  [data-fui-comp="ui-pane-host"],
  [data-fui-comp="ui-pane-host"].fui-pane-host--secondary-open,
  [data-fui-comp="ui-pane-host"].fui-pane-host--tertiary-open,
  [data-fui-comp="ui-pane-host"].fui-pane-host--secondary-open.fui-pane-host--tertiary-open,
  [data-fui-comp="ui-pane-host"][data-hui-pane-open~="secondary"],
  [data-fui-comp="ui-pane-host"][data-hui-pane-open~="tertiary"],
  [data-fui-comp="ui-pane-host"][data-hui-pane-open~="secondary"][data-hui-pane-open~="tertiary"] {
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
  border-inline-start: 1px solid var(--color-border, rgba(0, 0, 0, 0.10));
  box-shadow: var(--shadow-md, 0 10px 30px rgba(0, 0, 0, 0.18));
  overflow: auto;
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
