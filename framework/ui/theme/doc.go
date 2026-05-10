// Package theme is the canonical home for the framework's visual
// design system.
//
// The theme provides curated tokens — colors, spacing, radii, fonts —
// that every framework/ui component references via CSS custom
// properties. To re-skin an app, pass overrides to [Default]; every
// component re-resolves to the new values without code changes.
//
// Tokens are single-tier semantic: names carry meaning ("primary",
// "danger", "surface-soft") rather than raw values ("indigo-500"). If
// you need a deeper layering, build it on top — but most apps don't.
//
// The output is a [style.Theme] (from core-ui/style), so this package
// composes cleanly with the existing stylesheet builder and any host
// that already consumes core-ui themes.
package theme
