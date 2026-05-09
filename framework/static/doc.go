// Package static implements static-site generation for a framework.App
// with a UIHost mounted on it. The Builder walks every registered route,
// runs the screen's Load(ctx) hook, renders to HTML, and writes the result
// to disk along with the runtime.js, compiled actions, and any static
// assets.
//
// Dynamic routes (paths containing ":param" segments) participate when the
// screen's component implements core-ui/app.StaticPathsProvider; the
// builder expands the pattern against each returned param map.
//
// Build output is suitable for any static host (S3, GitHub Pages,
// Cloudflare Pages, etc.). The runtime.js included alongside the HTML
// drives client-side navigation, form actions, and signal-driven updates
// after first paint, so behavior matches the SSR server.
package static
