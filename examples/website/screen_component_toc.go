package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type TOCScreen struct{}

func (s *TOCScreen) ScreenTitle() string { return "Table of Contents" }
func (s *TOCScreen) ScreenDescription() string {
	return "Auto-built nav from h2/h3 + scroll-position tracking."
}
func (s *TOCScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *TOCScreen) Render() render.HTML {
	// Long-form content so the TOC + scroll tracking has something to
	// chew on. Each section has a stable id so the harvested links
	// resolve.
	section := func(id, h, body string) render.HTML {
		return render.Tag("section", map[string]string{"id": id, "data-fui-toc-target": "true"},
			html.Heading(html.HeadingConfig{Level: 2, ID: id}, render.Text(h)),
			html.Paragraph(html.TextConfig{}, render.Text(body)),
		)
	}
	subsec := func(id, h, body string) render.HTML {
		return render.Tag("section", map[string]string{"id": id, "data-fui-toc-target": "true"},
			html.Heading(html.HeadingConfig{Level: 3, ID: id}, render.Text(h)),
			html.Paragraph(html.TextConfig{}, render.Text(body)),
		)
	}

	body := render.Tag("div", map[string]string{"id": "toc-content"},
		section("install", "Install", "Run go get to add the framework. Use the dev binary for local watch + reload."),
		subsec("install-prereqs", "Prerequisites", "Go 1.22+ and a POSIX shell. SQLite is optional."),
		subsec("install-quick", "Quickstart", "Clone the repo, run go test, then go run the example website to see everything live."),
		section("config", "Configuration", "Most settings live in a single Config struct read from the environment."),
		subsec("config-env", "Environment", "PORT, DATABASE_URL, LOG_LEVEL — the usual."),
		subsec("config-flags", "Flags", "All env vars have CLI-flag overrides for ad-hoc runs."),
		section("deploy", "Deployment", "Build a single binary, copy it, run it. No build step on the server."),
		subsec("deploy-docker", "Docker", "FROM scratch + the binary is enough for many setups."),
		subsec("deploy-systemd", "systemd", "A 10-line .service file gets you restarts + journal logs."),
		section("further", "Further reading", "Architecture docs, framework guide, runtime contract — all in /docs."),
	)

	demo := render.Tag("div", map[string]string{"class": "demo-toc-layout"},
		ui.TableOfContents(ui.TOCConfig{Target: "#toc-content", Sticky: true}),
		body,
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("Table of Contents")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Auto-built from <h2> / <h3> inside the Target selector. The runtime harvests headings after first paint, renders the link list, and tracks the currently-in-view section via IntersectionObserver.")),
		demoFrame(demo, `ui.TableOfContents(ui.TOCConfig{
    Target: "#article",
    Sticky: true,
    Levels: 0, // h2 + h3 by default
})`),
	)
}
