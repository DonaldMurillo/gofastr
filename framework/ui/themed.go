package ui

import (
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core/render"
)

// Themed wraps children in a <div class="fui-theme-<hash>"> so the
// CSS variable cascade applies a section-level theme override to
// every descendant. Components inside Themed read var(--color-…)
// as usual — the browser dereferences them against the override
// block, not the canonical :root.
//
// Use it for dark sections, branded callouts, multi-tenant
// re-skinning of one subtree without touching surrounding chrome.
//
//	var Dark = style.RegisterThemeOverride(darkTheme)
//
//	ui.Themed(Dark,
//	    ui.Section(ui.SectionConfig{Heading: "Settings"},
//	        ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary}),
//	    ),
//	)
//
// The override class block lives in /__gofastr/app.css; registering
// the same theme twice (same content) returns the same handle, so
// the CSS only ships once.
func Themed(ref style.ThemeRef, children ...render.HTML) render.HTML {
	return render.Tag("div", map[string]string{"class": ref.Class()}, children...)
}
