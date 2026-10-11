package app

// InterceptOverlayCSS returns the chrome for an intercepted route's
// overlay, the drawer or sheet that a soft navigation from a declared
// origin presents instead of a full page (see intercept.go).
//
// It lives here, beside the layout shells, for the same reason they do:
// the runtime module that mounts the overlay owns wiring, never styling,
// and an app must not have to ship CSS to make a framework feature look
// right. The UI host injects it only when a route actually declares an
// intercept, so pages that never intercept carry none of it.
//
// Every value is a theme token, so the overlay inherits an app's palette,
// spacing, and radii without an override. Presentation keys off each
// layer's data-cui-intercept-as, which the SERVER sets from the
// registered ScreenType (the client cannot pick its own chrome), so one
// stack can hold a sheet over a drawer.
func InterceptOverlayCSS() string {
	return `/* Intercepted-route overlay: a scrim over the page that stays
   mounted underneath, with each layer docked to an edge. */
[data-cui-intercept-overlay] {
  position: fixed;
  inset: 0;
  /* --z-modal is the framework's overlay tier (300), the same one
     ui.PaneHost's drawer and sticky's "modal" tier use. Anything lower
     paints UNDER app chrome — a sticky site header sits at 50 — and the
     top of the overlay gets clipped. */
  z-index: var(--z-modal, 300);
  background: var(--ui-intercept-overlay-bg, rgba(0, 0, 0, 0.45));
  /* A record's form needs room: half the viewport, between 480px and
     720px, unless the theme's knob says otherwise. */
  --cui-intercept-drawer-w: var(--ui-intercept-drawer-w, clamp(480px, 50vw, 720px));
}
/* Every layer is positioned on its own, so stacked layers overlap
   instead of sharing the host's width. */
[data-cui-intercept-overlay] > * {
  position: absolute;
  background-color: var(--color-surface, #fff);
  color: var(--color-text, #18181b);
  overflow-y: auto;
  overscroll-behavior: contain;
  /* The layer's inset, named so chrome inside it (ui.DrawerBar) can
     bleed to the layer's edges. */
  --cui-intercept-pad: clamp(calc(var(--spacing-sm, 4px) * 5), 3vw, var(--spacing-2xl, 32px));
  padding: var(--cui-intercept-pad);
  box-shadow: var(--ui-intercept-shadow, 0 10px 40px rgba(0, 0, 0, 0.25));
}
/* Stacked layers: only the top one is live; the runtime marks the rest
   inert. Every layer over another casts the scrim over everything under
   it (an outer spread shadow paints over earlier siblings, never over
   the layer itself), the way the first layer sits over the page's
   scrim, so each lower layer dims one step per layer above it. */
[data-cui-intercept-overlay] > * + * {
  box-shadow: 0 0 0 100vmax var(--ui-intercept-overlay-bg, rgba(0, 0, 0, 0.45)), var(--ui-intercept-shadow, 0 10px 40px rgba(0, 0, 0, 0.25));
}
/* Drawer: docked to the inline end, full height. Stacked drawers
   overlap there, each one a step narrower than the layer under it, so
   the lower layers stay visible as a strip at the inline start. */
[data-cui-intercept-overlay] > [data-cui-intercept-as="drawer"] {
  inset-block: 0;
  inset-inline-end: 0;
  width: min(var(--cui-intercept-drawer-w), 100%);
  border-inline-start: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
}
[data-cui-intercept-overlay] > [data-cui-intercept-as="drawer"]:nth-child(2) {
  width: min(calc(var(--cui-intercept-drawer-w) - var(--ui-intercept-stack-step, var(--spacing-2xl, 32px))), 100%);
}
[data-cui-intercept-overlay] > [data-cui-intercept-as="drawer"]:nth-child(3) {
  width: min(calc(var(--cui-intercept-drawer-w) - 2 * var(--ui-intercept-stack-step, var(--spacing-2xl, 32px))), 100%);
}
[data-cui-intercept-overlay] > [data-cui-intercept-as="drawer"]:nth-child(4) {
  width: min(calc(var(--cui-intercept-drawer-w) - 3 * var(--ui-intercept-stack-step, var(--spacing-2xl, 32px))), 100%);
}
/* Sheet: docked to the bottom, capped so the page stays visible above.
   Stacked sheets overlap at the bottom edge, full width. */
[data-cui-intercept-overlay] > [data-cui-intercept-as="sheet"] {
  inset-inline: 0;
  inset-block-end: 0;
  max-height: var(--ui-intercept-sheet-h, 85vh);
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
  border-start-start-radius: var(--radii-lg, 10px);
  border-start-end-radius: var(--radii-lg, 10px);
}
/* Below the drawer breakpoint a side drawer is a poor fit; present it
   as a sheet instead. Matches the pane-host collapse at the same width. */
@media (max-width: 768px) {
  /* :nth-child(n) outranks the per-depth widths above; stacked sheets
     overlap at the bottom edge, full width. */
  [data-cui-intercept-overlay] > [data-cui-intercept-as="drawer"]:nth-child(n) {
    inset-block-start: auto;
    inset-inline: 0;
    width: 100%;
    height: auto;
    max-height: var(--ui-intercept-sheet-h, 85vh);
    border-inline-start: none;
    border-top: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
    border-start-start-radius: var(--radii-lg, 10px);
    border-start-end-radius: var(--radii-lg, 10px);
  }
}
@media (prefers-reduced-motion: no-preference) {
  [data-cui-intercept-overlay] > * { animation: cui-intercept-in var(--duration-overlay-enter, 200ms) var(--easing-ease-out, ease-out); }
  @keyframes cui-intercept-in {
    from { transform: translateY(8px); opacity: var(--opacity-muted, 0.6); }
    to   { transform: none; opacity: 1; }
  }
}
`
}
