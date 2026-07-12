package ui

// PaneHost — a layout shell with an always-visible primary pane and one
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
// is in-page state — never a URL route (Hard Rule 1). Optional URL
// round-tripping is a future extension, out of scope for v1.
//
// Shape (mirrors DocLayout — a display:grid whose column count CSS
// keys off open-state modifier classes on the root, so no inline style
// is emitted and CSP stays strict):
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
// (data-fui-pane-open / -close / -swap) and the drawer collapse.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// PaneHostConfig configures a PaneHost.
type PaneHostConfig struct {
	// Primary is the always-visible main pane. Required — PaneHost
	// panics when it is empty (mirrors DataTable/DocLayout required
	// slots).
	Primary render.HTML
	// Secondary is the first optional side pane. When empty, no
	// secondary pane is rendered.
	Secondary render.HTML
	// Tertiary is the second optional side pane.
	Tertiary render.HTML

	// SecondaryOpen / TertiaryOpen set the SSR initial open state so
	// the first paint matches server state (Hard Rule 6) — e.g. a
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

	ID    string
	Class string
}

// PaneHost renders a primary pane plus one or two openable side panes.
//
// The root carries data-fui-pane-host (the runtime marker) and an open
// modifier class per open pane (ui-pane-host--secondary-open /
// --tertiary-open); CSS derives the grid column count from those
// classes. Each side pane is a labelled role="region" with
// data-fui-pane="secondary|tertiary"; a closed side pane carries
// hidden so first paint matches state.
func PaneHost(cfg PaneHostConfig) render.HTML {
	if cfg.Primary == "" {
		panic("ui: PaneHost requires Primary")
	}

	secondaryOpen := cfg.Secondary != "" && cfg.SecondaryOpen
	tertiaryOpen := cfg.Tertiary != "" && cfg.TertiaryOpen

	cls := "ui-pane-host"
	if secondaryOpen {
		cls += " ui-pane-host--secondary-open"
	}
	if tertiaryOpen {
		cls += " ui-pane-host--tertiary-open"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	rootAttrs := map[string]string{
		"class":              cls,
		"data-fui-pane-host": "",
	}
	if cfg.ID != "" {
		rootAttrs["id"] = cfg.ID
	}

	children := []render.HTML{render.Tag("div", map[string]string{
		"class":         "ui-pane-host__pane ui-pane-host__pane--primary",
		"data-fui-pane": "primary",
	}, cfg.Primary)}

	if cfg.Secondary != "" {
		children = append(children, paneSlot("secondary", "Secondary", cfg.SecondaryLabel, secondaryOpen, cfg.Secondary))
	}
	if cfg.Tertiary != "" {
		children = append(children, paneSlot("tertiary", "Tertiary", cfg.TertiaryLabel, tertiaryOpen, cfg.Tertiary))
	}

	return paneHostStyle.WrapHTML(render.Tag("div", rootAttrs, children...))
}

// paneSlot renders one side pane: a labelled role="region" that is
// hidden when closed so the first paint matches state. fallbackLabel
// names the region when the caller omits a label.
func paneSlot(name, fallbackLabel, label string, open bool, body render.HTML) render.HTML {
	aria := fallbackLabel
	if label != "" {
		aria = label
	}
	attrs := map[string]string{
		"class":         "ui-pane-host__pane ui-pane-host__pane--" + name,
		"data-fui-pane": name,
		"role":          "region",
		"aria-label":    aria,
	}
	if !open {
		attrs["hidden"] = ""
	}
	return render.Tag("div", attrs, body)
}

var paneHostStyle = registry.RegisterStyle("ui-pane-host", paneHostCSS)

func paneHostCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-pane-host"] {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--ui-pane-host-gap, var(--spacing-lg, 24px));
  align-items: start;
  position: relative;
}
[data-fui-comp="ui-pane-host"] .ui-pane-host__pane { min-width: 0; }
/* hidden must win over any pane display rule so a closed pane never
   claims a grid track or paints at first paint. */
[data-fui-comp="ui-pane-host"] [data-fui-pane][hidden] { display: none; }

/* Column count is driven by open-state modifier classes on the root,
   not inline style (strict CSP, Hard Rule 9b). */
[data-fui-comp="ui-pane-host"].ui-pane-host--secondary-open {
  grid-template-columns: minmax(0, 1fr) var(--ui-pane-host-secondary-w, 360px);
}
[data-fui-comp="ui-pane-host"].ui-pane-host--tertiary-open:not(.ui-pane-host--secondary-open) {
  grid-template-columns: minmax(0, 1fr) var(--ui-pane-host-tertiary-w, 300px);
}
[data-fui-comp="ui-pane-host"].ui-pane-host--secondary-open.ui-pane-host--tertiary-open {
  grid-template-columns: minmax(0, 1fr) var(--ui-pane-host-secondary-w, 360px) var(--ui-pane-host-tertiary-w, 300px);
}

[data-fui-comp="ui-pane-host"] .ui-pane-host__pane--secondary,
[data-fui-comp="ui-pane-host"] .ui-pane-host__pane--tertiary {
  background: var(--color-surface, transparent);
  border: 1px solid var(--color-border, rgba(0, 0, 0, 0.10));
  border-radius: var(--radii-md, 8px);
  padding: var(--spacing-md, 16px);
}

/* Narrow viewport: the grid collapses to a single column. An open side
   pane renders as a fixed overlay drawer when the runtime sets
   data-fui-pane-mode="overlay" on the host (it does so once
   matchMedia(max-width: 768px) matches AND a pane is open). The
   breakpoint literal here MUST match the MQ in runtime/src/panehost.js. */
@media (max-width: 768px) {
  [data-fui-comp="ui-pane-host"],
  [data-fui-comp="ui-pane-host"].ui-pane-host--secondary-open,
  [data-fui-comp="ui-pane-host"].ui-pane-host--tertiary-open,
  [data-fui-comp="ui-pane-host"].ui-pane-host--secondary-open.ui-pane-host--tertiary-open {
    grid-template-columns: minmax(0, 1fr);
  }
}

/* Drawer chrome while an open pane is in overlay mode. */
[data-fui-pane-mode="overlay"] [data-fui-pane="secondary"]:not([hidden]),
[data-fui-pane-mode="overlay"] [data-fui-pane="tertiary"]:not([hidden]) {
  position: fixed;
  inset-block: 0;
  inset-inline-end: 0;
  block-size: 100dvh;
  inline-size: min(90vw, var(--ui-pane-host-drawer-w, 420px));
  z-index: var(--z-modal, 300);
  border-radius: 0;
  border-inline-start: 1px solid var(--color-border, rgba(0, 0, 0, 0.10));
  box-shadow: var(--shadow-md, 0 10px 30px rgba(0, 0, 0, 0.18));
  overflow: auto;
}

/* Backdrop scrim while a drawer is open. The host's ::before covers the
   viewport; a click on it lands on the host itself, which the runtime
   treats as a dismiss (backdrop-click closes the topmost pane). It sits
   one step BELOW the drawer's modal tier: on the framework token scale
   --z-popover (400) is ABOVE --z-modal (300), so the scrim must key off
   --z-modal, not --z-popover, or it would paint over the drawer and eat
   every click inside it. */
[data-fui-pane-mode="overlay"]::before {
  content: "";
  position: fixed;
  inset: 0;
  background: var(--ui-pane-host-scrim, rgba(10, 9, 15, 0.42));
  z-index: calc(var(--z-modal, 300) - 1);
}`
}
