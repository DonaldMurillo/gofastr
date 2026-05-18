package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type MarkdownScreen struct{}

func (s *MarkdownScreen) ScreenTitle() string { return "Markdown" }
func (s *MarkdownScreen) ScreenDescription() string {
	return "Themed Markdown renderer."
}
func (s *MarkdownScreen) ScreenType() app.ScreenType { return app.ScreenPage }

const demoMarkdown = `# Hello, world

This is a **themed** Markdown render. Paragraphs, headings, lists, and code blocks all pick up theme tokens.

## Features

- Theme-token colors for links, code, and borders
- Native heading hierarchy
- Inline ` + "`code`" + ` styled to match CodeBlock
- Fenced code blocks render with the same dark inkwell as standalone code

` + "```go" + `
func main() {
    fmt.Println("hello")
}
` + "```" + `

> Pull-quote support — block-quote tints to surface-soft so it stands out without being loud.

| Field   | Type   |
|---------|--------|
| Name    | string |
| Email   | string |
| Active  | bool   |
`

func (s *MarkdownScreen) Render() render.HTML {
	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Markdown")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Themed wrapper over core/markdown. Wraps the rendered HTML in a prose container so headings, lists, links, code blocks, blockquotes, and tables all get theme-token styling without the caller writing CSS.")),
		demoFrame(ui.Markdown(ui.MarkdownConfig{Source: demoMarkdown}),
			`ui.Markdown(ui.MarkdownConfig{Source: source})`),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Compact mode")),
		demoFrame(ui.Markdown(ui.MarkdownConfig{Source: demoMarkdown, Compact: true}),
			`ui.Markdown(ui.MarkdownConfig{Source: source, Compact: true})`),
	)
}
