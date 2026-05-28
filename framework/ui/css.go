package ui

// BaseCSS used to return framework/ui's residual helpers (.ui-visually-hidden
// and a few others). That responsibility moved into the uihost's auto-emitted
// app.css floor — every app gets the helpers without opting in, and any
// component that needs more CSS owns it via a registry.Style handle.
//
// BaseCSS now returns "" and is kept only so apps that still pass
// ui.BaseCSS() to uihost.WithCustomCSS keep compiling. Drop the call when
// convenient; it's a no-op.
//
// Deprecated: not needed; included for back-compat. Will be removed in a
// future minor release.
func BaseCSS() string { return "" }
