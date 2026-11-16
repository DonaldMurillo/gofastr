package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Themed wraps children in a <div> carrying ref's class, so the registered
// theme override applies to them.
func Themed(ref style.ThemeRef, children ...render.HTML) render.HTML {
	return render.Tag("div", map[string]string{"class": ref.Class()}, children...)
}
