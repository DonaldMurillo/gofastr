// Package accordion provides disclosure widgets built on the native
// <details>/<summary> html.
//
// Two variants are exported:
//
//   - [Group] — exclusive: at most one item open at a time, achieved with
//     the native `name=` attribute on <details>. No JS required.
//   - [Stack] — independent: items open and close on their own.
//
// Both variants render fully on the server, are keyboard accessible by
// default (Enter/Space toggle, Tab moves focus between summaries), and
// animate via modern CSS only — interpolate-size: allow-keywords,
// ::details-content, and transition-behavior: allow-discrete.
//
// Browsers without these features get instant open/close, which is an
// acceptable progressive-enhancement fallback.
//
// The animation styles load automatically. This package registers its
// stylesheet at init via [registry.RegisterStyle] (the package-level
// [Style] handle), and [Group]/[Stack] wrap their output in
// [registry.Style.WrapHTML], which stamps data-fui-comp="accordion" onto
// the outer tag. On first paint the SSR host emits a scoped <link> in
// <head>; after hydration the runtime loads the CSS on demand and dedups
// it. No app-startup wiring and no stylesheet concatenation — see
// core-ui/ARCHITECTURE.md "Component CSS" for the registry path.
package accordion
