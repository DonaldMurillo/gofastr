// Package widget provides the framework's overlay-UI primitive.
//
// A widget is a self-mounting UI surface that runs on top of any page —
// regardless of whether that page is built with core-ui components or
// is a plain HTML document. Widgets are distinct from components:
//
//   - Components are part of a server-rendered page tree.
//   - Widgets are siblings that float above the page (fixed corners,
//     modal overlays, toast stacks). They mount themselves via a
//     bootstrap script tag the framework generates.
//
// Architectural goals:
//
//  1. Hosts (kiln, future feature packages) describe widgets
//     declaratively via WidgetDef. They never write DOM/CSS by hand.
//
//  2. The runtime (core-ui/runtime + core-ui/html/Overlay) owns
//     mounting, stacking, focus management, SSE wiring, and RPC
//     dispatch. Hosts only fill slots and declare server-side signals
//     + RPC handlers.
//
//  3. Theming flows through core-ui/style. A widget never embeds
//     hex colors or magic spacing; it references theme tokens
//     ({colors.primary}, {spacing.lg}). Override the theme to reskin
//     every widget at once.
//
// Anatomy of a widget — code skeleton:
//
//	def := widget.New("kiln-panel").
//	    Mount(widget.BottomRight).
//	    Slot("header", headerComponent).
//	    Slot("body",   bodyComponent).
//	    Signal("page", pageSignalSrc).
//	    SSE("/.kiln/events", "world_edit", "page").
//	    RPC("POST", "/kiln/tool/reset_session", resetHandler)
//
//	widget.Mount(app, def)
//
// The package is intentionally small in interface. Built-in surfaces
// (FloatingPanel, Modal, Toast, Drawer, Popover) live in
// core-ui/widget/preset and are constructed with the same WidgetDef
// builder — no new primitive per surface.
package widget
