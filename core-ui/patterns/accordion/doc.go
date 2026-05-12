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
// Call [BaseCSS] once at app startup and append it to your stylesheet
// (typically via uihost.WithCustomCSS(...)) to enable the animation
// styles. Authors who prefer per-component scoped CSS with lazy load
// can wrap their own Accordion with [registry.RegisterStyle] +
// [registry.Style.WrapHTML] — see core-ui/ARCHITECTURE.md "Component
// CSS" for the registry path.
package accordion
