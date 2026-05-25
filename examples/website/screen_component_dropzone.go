package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type DropzoneScreen struct{}

func (s *DropzoneScreen) ScreenTitle() string { return "File Dropzone" }
func (s *DropzoneScreen) ScreenDescription() string {
	return "Hero file-drop surface with optional image previews."
}
func (s *DropzoneScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *DropzoneScreen) Render() render.HTML {
	basic := html.Form(html.FormConfig{Method: "post", Action: "#", ExtraAttrs: html.Attrs{"enctype": "multipart/form-data"}},
		ui.FileDropzone(ui.FileDropzoneConfig{
			Name:      "import",
			Label:     "Import CSV",
			Accept:    ".csv,text/csv",
			MaxSizeMB: 10,
			Help:      "CSV must include header row.",
		}),
	)

	withPreview := html.Form(html.FormConfig{Method: "post", Action: "#", ExtraAttrs: html.Attrs{"enctype": "multipart/form-data"}},
		ui.FileDropzone(ui.FileDropzoneConfig{
			Name:        "photos",
			Label:       "Upload photos",
			Accept:      "image/*",
			Multiple:    true,
			ShowPreview: true,
			MaxSizeMB:   5,
			Help:        "JPG / PNG / WebP. Up to 12 at once.",
		}),
	)

	src := `ui.FileDropzone(ui.FileDropzoneConfig{
    Name:      "import",
    Label:     "Import CSV",
    Accept:    ".csv,text/csv",
    MaxSizeMB: 10,
    Help:      "CSV must include header row.",
})`

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("File Dropzone")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Hero file-drop surface. Native <input type=\"file\"> for keyboard / SR / no-JS form submit; the dropzone runtime adds drag-drop forwarding (via the existing data-fui-fileupload hook), filename display after pick, and optional image previews via FileReader.")),
		// FileDropzone renders its own <h3> Label inside the zone — an
		// h2 between this page h1 and the dropzone h3 keeps the WCAG
		// 1.3.1 heading order monotonic.
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Basic")),
		demoFrame(basic, src),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("With image preview strip")),
		demoFrame(withPreview, `ui.FileDropzone(ui.FileDropzoneConfig{
    Name:        "photos",
    Label:       "Upload photos",
    Accept:      "image/*",
    Multiple:    true,
    ShowPreview: true,
})`),
	)
}
