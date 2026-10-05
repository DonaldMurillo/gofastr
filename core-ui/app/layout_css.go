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
// spacing, and radii without an override. Presentation keys off
// data-cui-intercept-as, which the SERVER sets from the registered
// ScreenType, the client cannot pick its own chrome.
func InterceptOverlayCSS() string {
	return `/* Intercepted-route overlay: a scrim over the page that stays
   mounted underneath, with the screen render docked to an edge. */
[data-cui-intercept-overlay] {
  position: fixed;
  inset: 0;
  /* --z-modal is the framework's overlay tier (300), the same one
     ui.PaneHost's drawer and sticky's "modal" tier use. Anything lower
     paints UNDER app chrome — a sticky site header sits at 50 — and the
     top of the overlay gets clipped. */
  z-index: var(--z-modal, 300);
  display: flex;
  background: var(--ui-intercept-overlay-bg, rgba(0, 0, 0, 0.45));
}
[data-cui-intercept-overlay] > * {
  background-color: var(--color-surface, #fff);
  color: var(--color-text, #18181b);
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: clamp(calc(var(--spacing-sm, 4px) * 5), 3vw, var(--spacing-2xl, 32px));
  box-shadow: var(--ui-intercept-shadow, 0 10px 40px rgba(0, 0, 0, 0.25));
}
/* Drawer: docked to the inline end, full height. */
[data-cui-intercept-as="drawer"] { justify-content: flex-end; }
[data-cui-intercept-as="drawer"] > * {
  width: min(var(--ui-intercept-drawer-w, 480px), 100%);
  height: 100%;
  border-inline-start: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
}
/* Sheet: docked to the bottom, capped so the page stays visible above. */
[data-cui-intercept-as="sheet"] { align-items: flex-end; }
[data-cui-intercept-as="sheet"] > * {
  width: 100%;
  max-height: var(--ui-intercept-sheet-h, 85vh);
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
  border-start-start-radius: var(--radii-lg, 10px);
  border-start-end-radius: var(--radii-lg, 10px);
}
/* Below the drawer breakpoint a side drawer is a poor fit; present it
   as a sheet instead. Matches the pane-host collapse at the same width. */
@media (max-width: 768px) {
  [data-cui-intercept-as="drawer"] { align-items: flex-end; justify-content: stretch; }
  [data-cui-intercept-as="drawer"] > * {
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
  [data-cui-intercept-overlay] > * { animation: cui-intercept-in var(--duration-overlay-enter, 200ms) ease-out; }
  @keyframes cui-intercept-in {
    from { transform: translateY(8px); opacity: var(--opacity-muted, 0.6); }
    to   { transform: none; opacity: 1; }
  }
}
`
}
