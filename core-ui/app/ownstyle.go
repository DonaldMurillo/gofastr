package app

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
)

// OwnedStyle is an owned style handle: the value `gofastr gen styles`
// generates from a <name>.style.css file. The handle embeds
// *ownstyle.Sheet, which supplies OwnedSheet.
//
// Three owners take one here. LayoutSpec.Style and Screen.WithStyle
// take a scoped sheet; the framework stamps data-fui-scope="<name>" on
// the owner's root, which bounds the compiled @scope and tells the
// SSR head and the runtime to load /__gofastr/comp/<name>.css.
// App.WithStyle takes the app sheet, which covers every page and
// always loads.
type OwnedStyle interface {
	OwnedSheet() *ownstyle.Sheet
}

// scopedSheet unwraps st for a layout or screen owner, panicking with
// the fix when it is nil or the app sheet.
func scopedSheet(owner string, st OwnedStyle) *ownstyle.Sheet {
	sh := ownedSheet(owner, st)
	if sh.Kind() == ownstyle.KindApp {
		panic(fmt.Sprintf("app: %s: the app style covers every page and has no root element; pass it to App.WithStyle, and give the %s a scoped style (a <name>.style.css file)", owner, owner))
	}
	return sh
}

func ownedSheet(owner string, st OwnedStyle) *ownstyle.Sheet {
	if st == nil {
		panic(fmt.Sprintf("app: %s: nil style; pass the handle `gofastr gen styles` generated", owner))
	}
	sh := st.OwnedSheet()
	if sh == nil {
		panic(fmt.Sprintf("app: %s: the style handle holds no sheet; pass the handle `gofastr gen styles` generated", owner))
	}
	return sh
}

// WithStyle gives the screen an owned style. The screen's content is
// wrapped once — in its <article> when the screen is an article,
// otherwise in a plain <div> — and the wrapper carries
// data-fui-scope="<name>". The primary cell is never the scope root:
// it persists across navigations while screens swap inside it. Fills
// render in the layout's outlets, outside the wrapper, so the screen's
// style never reaches them.
//
// Panics on a nil handle or the app style.
func (s *Screen) WithStyle(st OwnedStyle) *Screen {
	s.ownStyle = scopedSheet(fmt.Sprintf("screen %q WithStyle", s.Path), st)
	return s
}

// WithStyle declares the app's own style (app.style.css): rules that
// cover every page. The sheet always loads, so this records the
// declaration and checks its kind; a scoped style panics with the fix.
func (a *App) WithStyle(st OwnedStyle) *App {
	sh := ownedSheet("App.WithStyle", st)
	if sh.Kind() != ownstyle.KindApp {
		panic(fmt.Sprintf("app: App.WithStyle: %q is a scoped style; give it to a layout (LayoutSpec.Style), a screen (Screen.WithStyle) or a component (Style.Scope). The app style is app.style.css", sh.Name()))
	}
	a.ownStyle = sh
	return a
}
