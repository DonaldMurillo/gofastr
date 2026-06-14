package app

// LayoutBaseCSS returns the structural CSS for the layout shells the app
// package emits (.layout-body, the sidebar row, and the WithContainer centered
// column). It's owned here — next to the markup it styles — and injected once by
// the UI host, so neither apps nor generators ship layout CSS of their own. All
// colors/widths reference theme tokens (--color-*, --ui-layout-*), overridable
// per app.
func LayoutBaseCSS() string {
	return `/* Layout body: sidebar column beside the content as a flex row at >= md;
   stacks at < md where the sidebar collapses to a hamburger drawer. */
.layout-body { display: flex; align-items: stretch; min-height: 100vh; }
.layout-body > main, .layout-body > .layout-content { flex: 1 1 auto; min-width: 0; }
.layout-body > nav { flex: 0 0 auto; background-color: var(--color-surface, #fff); border-right: 1px solid var(--color-border, #e4e4e7); }
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

/* Sidebar shell: a padded content area beside the nav. */
.layout--has-sidebar main, .layout--has-sidebar .layout-content {
  display: flex;
  flex-direction: column;
  gap: 24px;
  padding: clamp(24px, 3vw, 40px);
}
`
}
