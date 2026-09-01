package app

// LayoutBaseCSS returns the structural CSS for the layout shells the app
// package emits (.layout-body, the sidebar row, and the WithContainer centered
// column). It's owned here, next to the markup it styles, and injected once by
// the UI host, so neither apps nor generators ship layout CSS of their own. All
// colors/widths reference theme tokens (--color-*, --ui-layout-*), overridable
// per app.
func LayoutBaseCSS() string {
	return `/* Layout body: sidebar column beside the content as a flex row at >= md;
   stacks at < md where the sidebar collapses to a hamburger drawer. */
.layout-body { display: flex; align-items: stretch; min-height: 100vh; }
.layout-body > main, .layout-body > .layout-content { flex: 1 1 auto; min-width: 0; }
.layout-body > nav { flex: 0 0 auto; background-color: var(--color-surface, #fff); border-right: 1px solid var(--color-border, #e4e4e7); }

/* Embed layout (EmbedLayout): sizes to its CONTENT, never to the viewport.
   An embedded surface lives in an iframe that the host page resizes to the
   height the frame reports. With min-height: 100vh that reported height is
   partly the frame's own height, so each report grows the frame, which grows
   100vh, which grows the next report — the panel ratchets open with a band of
   empty space under the content. Caught in a screenshot; invisible to any DOM
   assertion, because every element is present and correct. */
.layout-embed .layout-body { min-height: 0; }
@media (max-width: 47.99rem) {
  .layout-body { display: block; }
  .layout-body > nav { border-right: none; border-bottom: 1px solid var(--color-border, #e4e4e7); }
}

/* Contained layout (WithContainer): a centered editorial column for the
   content, while the header and footer span FULL WIDTH (edge-to-edge border)
   with their inner content centered to the same measure — the normal marketing
   header shape. The wrapper itself is full-width so its background/borders go
   end to end; the centering is per-region. The flex column keeps the footer at
   the bottom. --ui-layout-container-width sets the measure (default 66rem),
   --ui-layout-gutter the minimum side gutter. */
.layout--contained {
  --ui-layout-gutter: clamp(20px, 5vw, 32px);
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}
.layout--contained .layout-body { display: block; min-height: 0; flex: 1 0 auto; }
/* Content column: centered to the measure. */
.layout--contained main, .layout--contained .layout-content {
  inline-size: 100%;
  max-inline-size: var(--ui-layout-container-width, 66rem);
  margin-inline: auto;
  padding-inline: var(--ui-layout-gutter);
  display: flex;
  flex-direction: column;
  gap: clamp(24px, 4vw, 36px);
  padding-block: clamp(40px, 6vw, 64px) clamp(48px, 7vw, 80px);
}
/* Sections and the hero get extra room above; prose keeps the tighter rhythm.
   Screens wrap their blocks in a div, so match both main>… and main>div>…. */
.layout--contained main > [data-fui-comp="ui-section"]:not(:first-child),
.layout--contained main > [data-fui-comp="ui-hero"]:not(:first-child),
.layout--contained main > div > [data-fui-comp="ui-section"]:not(:first-child),
.layout--contained main > div > [data-fui-comp="ui-hero"]:not(:first-child) { margin-top: clamp(24px, 4vw, 48px); }
/* Header + footer: full-bleed band, content centered to the measure by padding
   the difference (so the bottom border still spans edge to edge). */
.layout--contained [data-fui-comp="ui-site-header"],
.layout--contained [data-fui-comp="ui-site-footer"] {
  padding-inline: max(var(--ui-layout-gutter), calc((100% - var(--ui-layout-container-width, 66rem)) / 2));
}
.layout--contained [data-fui-comp="ui-site-header"] {
  block-size: auto;
  padding-block: clamp(14px, 3vw, 22px);
  border-bottom: 1px solid var(--color-border, #e4e4e7);
}
.layout--contained [data-fui-comp="ui-site-header"] .ui-site-header__links { margin-inline-start: auto; }
/* Long-form content prose (heading + paragraph blocks live in the screen's
   wrapper div): a comfortable measure + vertical rhythm so /about, /terms read
   like prose, not a full-width wall. */
.layout--contained main > h1, .layout--contained main > div > h1 {
  font-size: clamp(2rem, 4vw, 2.75rem); line-height: 1.1; letter-spacing: -0.02em; max-width: 24ch; margin: 0;
}
.layout--contained main > p, .layout--contained main > div > p {
  max-width: 68ch; line-height: 1.7; margin: 0;
}
.layout--contained main > div > h1 + p,
.layout--contained main > div > p + p { margin-top: 1.1rem; }
/* The markdown content block keeps its own measure on long-form pages. */
.layout--contained main > div > [data-fui-comp="ui-markdown"] { max-width: 72ch; }

/* Sidebar shell banner: the contained layout styles its header above, but a
   WithSidebar + WithHeader shell needs its own chrome — ui.SiteHeader is
   block-size: 100%, which resolves against a parent with no height, so
   without this rule the banner collapses to text height over the bare page
   background. --ui-layout-header-height follows the --ui-layout-* override
   convention (see --ui-layout-container-width). */
.layout--has-sidebar > header {
  display: flex;
  align-items: stretch;
  block-size: var(--ui-layout-header-height, 56px);
  background-color: var(--color-surface, #fff);
  border-bottom: 1px solid var(--color-border, #e4e4e7);
}

/* Sticky header (WithStickyHeader): a header component cannot stick to the
   page from inside the wrapper, because a sticky element only travels
   inside its parent's box and the component's parent is the wrapper, whose
   height is the header itself. The WRAPPER is the element that sticks.
   Background: the wrapper is transparent by default, so without one the
   page content scrolls visibly underneath it. --ui-layout-header-bg
   follows the --ui-layout-* override convention; stacking uses the
   theme's --z-sticky layer (200, above app chrome, below overlays). */
.layout--sticky-header > header {
  position: sticky;
  top: 0;
  z-index: var(--z-sticky, 200);
  background-color: var(--ui-layout-header-bg, var(--color-background, #fff));
}
/* A sticky header makes the shell's dead scroll visible. .layout-body is
   min-height: 100vh, so a header above it means a short page scrolls by
   exactly the header's height with nothing in the overflow — under a
   pinned header that reads as broken rather than as a stray pixel. The
   contained and sidebar shells each fix this their own way already
   (min-height: 0 there, a calc() subtracting a FIXED header height here);
   a flex column fixes it for a header of any height, which is what a
   caller-supplied component is. */
.layout--sticky-header {
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}
.layout--sticky-header > .layout-body {
  flex: 1 0 auto;
  min-height: 0;
}
/* .layout-body is min-height: 100vh, so a banner above it makes every page
   scroll by exactly the header height with nothing in the overflow; the
   body under a sidebar-shell banner subtracts it. */
.layout--has-sidebar > header + .layout-body {
  min-height: calc(100vh - var(--ui-layout-header-height, 56px));
}

/* Sidebar shell: a padded content area beside the nav. */
.layout--has-sidebar main, .layout--has-sidebar .layout-content {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xl, 24px);
  padding: clamp(24px, 3vw, 40px);
}
`
}

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
// data-fui-intercept-as, which the SERVER sets from the registered
// ScreenType, the client cannot pick its own chrome.
func InterceptOverlayCSS() string {
	return `/* Intercepted-route overlay: a scrim over the page that stays
   mounted underneath, with the screen render docked to an edge. */
[data-fui-intercept-overlay] {
  position: fixed;
  inset: 0;
  /* --z-modal is the framework's overlay tier (300), the same one
     ui.PaneHost's drawer and sticky's "modal" tier use. Anything lower
     paints UNDER app chrome — a sticky site header sits at 50 — and the
     top of the overlay gets clipped. */
  z-index: var(--z-modal, 300);
  display: flex;
  background: var(--color-overlay, rgba(0, 0, 0, 0.45));
}
[data-fui-intercept-overlay] > * {
  background-color: var(--color-surface, #fff);
  color: var(--color-text, #18181b);
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: clamp(20px, 3vw, 32px);
  box-shadow: var(--ui-intercept-shadow, 0 10px 40px rgba(0, 0, 0, 0.25));
}
/* Drawer: docked to the inline end, full height. */
[data-fui-intercept-as="drawer"] { justify-content: flex-end; }
[data-fui-intercept-as="drawer"] > * {
  width: min(var(--ui-intercept-drawer-w, 480px), 100%);
  height: 100%;
  border-inline-start: 1px solid var(--color-border, #e4e4e7);
}
/* Sheet: docked to the bottom, capped so the page stays visible above. */
[data-fui-intercept-as="sheet"] { align-items: flex-end; }
[data-fui-intercept-as="sheet"] > * {
  width: 100%;
  max-height: var(--ui-intercept-sheet-h, 85vh);
  border-top: 1px solid var(--color-border, #e4e4e7);
  border-start-start-radius: var(--radius-lg, 12px);
  border-start-end-radius: var(--radius-lg, 12px);
}
/* Below the drawer breakpoint a side drawer is a poor fit; present it
   as a sheet instead. Matches the pane-host collapse at the same width. */
@media (max-width: 768px) {
  [data-fui-intercept-as="drawer"] { align-items: flex-end; justify-content: stretch; }
  [data-fui-intercept-as="drawer"] > * {
    width: 100%;
    height: auto;
    max-height: var(--ui-intercept-sheet-h, 85vh);
    border-inline-start: none;
    border-top: 1px solid var(--color-border, #e4e4e7);
    border-start-start-radius: var(--radius-lg, 12px);
    border-start-end-radius: var(--radius-lg, 12px);
  }
}
@media (prefers-reduced-motion: no-preference) {
  [data-fui-intercept-overlay] > * { animation: fui-intercept-in 160ms ease-out; }
  @keyframes fui-intercept-in {
    from { transform: translateY(8px); opacity: 0.6; }
    to   { transform: none; opacity: 1; }
  }
}
`
}
