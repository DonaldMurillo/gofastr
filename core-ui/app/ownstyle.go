package app

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
)

// OwnedStyle is an owned style handle: the value `gofastr gen styles` generates from a <name>.style.css file.
type OwnedStyle interface {
	OwnedSheet() *ownstyle.Sheet
}

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

// WithStyle gives the screen an owned style.
func (s *Screen) WithStyle(st OwnedStyle) *Screen {
	s.ownStyle = scopedSheet(fmt.Sprintf("screen %q WithStyle", s.Path), st)
	return s
}

// WithStyle declares the app's own style (app.style.css): rules that cover every page.
func (a *App) WithStyle(st OwnedStyle) *App {
	sh := ownedSheet("App.WithStyle", st)
	if sh.Kind() != ownstyle.KindApp {
		panic(fmt.Sprintf("app: App.WithStyle: %q is a scoped style; give it to a layout (LayoutSpec.Style), a screen (Screen.WithStyle) or a component (Style.Scope). The app style is app.style.css", sh.Name()))
	}
	a.ownStyle = sh
	return a
}
