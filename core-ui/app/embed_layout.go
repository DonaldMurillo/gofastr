package app

// EmbedLayoutName is the layout name embedded surfaces render under. It lands
// in the wrapper's class and in data-fui-layout, so it is part of the CSS
// contract rather than a private string.
const EmbedLayoutName = "embed"

// EmbedLayout returns the chrome-less layout embedded surfaces render under.
//
// A normal layout carries a site header, nav, a footer and skip links. Inside a
// 400px-wide iframe on someone else's site all of that is wrong: the customer's
// page already has a header, and a second one framed inside their content reads
// as a broken page rather than as an integration.
//
// So the embed layout has no chrome, only the <main> landmark. It is a real
// layout rather than a flag on the embed route for two reasons. An app author
// can register a screen with it and see locally exactly what their customer
// will see; and the landmark stays in one place, so the frame document has
// exactly one <main> however it was rendered. (A route flag that suppressed
// chrome would have produced either no landmark or a second one, and a nested
// <main> is an accessibility violation the site's own axe gate catches.)
//
// It contributes no CSS of its own: the generic layout shape in LayoutBaseCSS
// already covers a header-less, sidebar-less shell, and everything inside comes
// from the components the screen renders.
func EmbedLayout() *Layout {
	return NewLayout(EmbedLayoutName)
}
