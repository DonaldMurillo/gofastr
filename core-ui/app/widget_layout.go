package app

// WidgetLayoutName is the layout name floating desktop widgets render
// under. It lands in the wrapper's class and in data-fui-layout, so it
// is part of the CSS contract rather than a private string.
const WidgetLayoutName = "widget"

// WidgetLayout returns the chrome-less, transparent layout a floating
// desktop widget renders under.
//
// A widget is a small borderless window (battery/desktop's
// desktop.Widget): no title bar, a transparent window background, often
// pinned above other apps. Inside it the app layout is wrong three
// times over: the site header and footer have nowhere to go, the
// contained column's padding eats a 320-point window, and the page's
// own background paints an opaque rectangle behind whatever the screen
// draws, so the "transparent" window shows a white slab with the
// theme's corners cut off. Caught in a screenshot of the first widget;
// invisible to any DOM assertion.
//
// So the widget layout has no chrome, only the <main> landmark, and
// LayoutBaseCSS makes the page transparent behind it and drops the
// viewport-height floor, so the screen's own surface (a ui.Card, say)
// is the whole visible window. --ui-layout-widget-padding is the gap
// between the window edge and that surface (default 8px), overridable
// per app like the other --ui-layout-* variables.
func WidgetLayout() *Layout {
	return NewLayout(WidgetLayoutName)
}
