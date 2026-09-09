// Package ui is the desktop theme and its components: the macOS look
// from phase 13 of docs/desktop-plan.md, shipped as design-system
// extensions from the battery instead of edits to framework/ui or
// core-ui.
//
// The theme is named desktop and its token values are per platform:
// macOS values now, Windows and Linux later swap values, never fields.
// Everything here registers through the design system's own machinery
// (style.Theme, registry.RegisterStyle, core-ui/app layouts), so a host
// that opts in composes the same single styling surface every web app
// uses.
//
// The page-side contract with the native shell is two classes on
// <html>, set by the desktop runtime module (never by this package):
//
//	desktop-reduce-transparency  the user turned on Reduce Transparency;
//	                             glass surfaces go opaque, no filter
//	desktop-inactive             the window lost key status; glass fills
//	                             flatten and the accent dims
//
// WebKit has no prefers-reduced-transparency query and no CSS signal
// for window activity, so the shell pushes both states as classes.
package desktopui
