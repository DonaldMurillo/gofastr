package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type DiffViewerScreen struct{}

func (s *DiffViewerScreen) ScreenTitle() string { return "Diff Viewer" }
func (s *DiffViewerScreen) ScreenDescription() string {
	return "Unified or split diff renderer."
}
func (s *DiffViewerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

const demoPatch = `--- a/handler.go
+++ b/handler.go
@@ -1,8 +1,9 @@
 package handler

-import "fmt"
+import "log/slog"
+import "net/http"

-func Hello() {
-	fmt.Println("hi")
+func Hello(w http.ResponseWriter, _ *http.Request) {
+	slog.Info("greeted")
 }
`

func (s *DiffViewerScreen) Render() render.HTML {
	unified := ui.DiffViewer(ui.DiffViewerConfig{Patch: demoPatch})
	split := ui.DiffViewer(ui.DiffViewerConfig{
		Patch: demoPatch, Mode: ui.DiffSplit,
		LeftLabel: "main", RightLabel: "feature",
	})

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Diff Viewer")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Renders unified-diff text into a styled view. Two modes: Unified (single column, +/− prefix) and Split (two columns, removed on left, added on right). Hunk headers (@@) and file headers (--- / +++) are styled separately.")),
		demoFrame(unified, `ui.DiffViewer(ui.DiffViewerConfig{Patch: patch})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Split mode")),
		demoFrame(split, `ui.DiffViewer(ui.DiffViewerConfig{
    Patch: patch, Mode: ui.DiffSplit,
    LeftLabel: "main", RightLabel: "feature",
})`),
	)
}
