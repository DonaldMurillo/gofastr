package main

// =============================================================================
// Site identity shared by the chrome and the SEO surfaces.
//
// The header and footer themselves are the site's own owned-style
// packages now (examples/site/siteheader, examples/site/sitefooter):
// their markup, their stylesheets, their tokens. main.go's layout wires
// them around the primary slot; see those packages for the contract.

import (
	"github.com/DonaldMurillo/gofastr/examples/site/sitefooter"
	"github.com/DonaldMurillo/gofastr/examples/site/siteheader"
)

// build time from the deployment's git tag via
//
//	-ldflags "-X 'main.siteVersion=$(git describe --tags --abbrev=0 | sed s/^v//)'"
//
// (see scripts/dev-watch.sh, Makefile build-examples, .github/workflows/pages.yml).
// The "dev" fallback is what an un-injected `go build`/`go run` shows locally,
// so the deployed site always matches the tag it was built from, instead of a
// hand-bumped constant drifting behind releases.
var siteVersion = "dev"

// siteOrigin is the public origin the site declares to crawlers and agents:
// og:url, the sitemap's BaseURL, and (via resolveBaseURL) every agent-ready
// discovery URL. It is a variable, not a constant, so a deployment can point
// it at its own hostname at build time via
//
//	-ldflags "-X 'main.siteOrigin=https://example.com'"
//
// The default is the origin the site is actually served from today. It used
// to be hardcoded to a domain that resolves to nothing, which pointed every link
// preview, sitemap entry, and agent card at a dead host, the one class of SEO
// bug that no page-level test catches, because each page was internally
// consistent with the wrong origin.
var siteOrigin = "https://donaldmurillo.github.io/gofastr"

// versionLabel renders siteVersion for display: bare for the local "dev"
// fallback, "v"-prefixed for a real injected release (e.g. "v0.8.0"). Keeps
// the brand badge + aria-label from ever reading the malformed "vdev".
func versionLabel() string {
	if siteVersion == "" || siteVersion == "dev" {
		return siteVersion
	}
	return "v" + siteVersion
}

// siteInstallTarget keeps the public install command reproducible. Deployed
// builds receive the release tag through siteVersion; local source builds
// intentionally point at main rather than pretending @latest is pinned.
func siteInstallTarget() string {
	if siteVersion == "" || siteVersion == "dev" {
		return "main"
	}
	return versionLabel()
}

// siteNavLinks is the primary navigation, shared by the header package's
// desktop nav and phone menu: the seven content sections, each lit on every
// page under its prefix.
func siteNavLinks() []siteheader.Link {
	return []siteheader.Link{
		{Label: "Primitives", Href: "/primitives", Section: true},
		{Label: "Framework", Href: "/framework", Section: true},
		{Label: "Agents", Href: "/agents", Section: true},
		{Label: "Interactivity", Href: "/interactivity", Section: true},
		{Label: "Generator", Href: "/generator", Section: true},
		{Label: "Examples", Href: "/examples", Section: true},
		{Label: "Plugins", Href: "/plugins", Section: true},
	}
}

// siteNavExtraLinks render only in the header's phone menu: the destinations
// a thumb expects first, plus the repo.
func siteNavExtraLinks() []siteheader.Link {
	return []siteheader.Link{
		{Label: "Home", Href: "/"},
		{Label: "Docs (all)", Href: "/docs/", Section: true},
		{Label: "Get started", Href: "/get-started", Section: true},
		{Label: "GitHub ↗", Href: "https://github.com/DonaldMurillo/gofastr", External: true},
	}
}

// siteFooterColumns is the colophon's link columns.
func siteFooterColumns() []sitefooter.Column {
	return []sitefooter.Column{
		{Title: "Read", Links: []sitefooter.Link{
			{Label: "Get started", Href: "/get-started"},
			{Label: "Docs", Href: "/docs/"},
			{Label: "Philosophy", Href: "/philosophy"},
			{Label: "Journal", Href: "https://github.com/DonaldMurillo/gofastr/commits/main", External: true},
		}},
		{Title: "Use", Links: []sitefooter.Link{
			{Label: "Examples", Href: "/examples"},
			{Label: "Plugins", Href: "/plugins"},
			{Label: "Kiln (experimental)", Href: "/kiln"},
			{Label: "CLI", Href: "https://pkg.go.dev/github.com/DonaldMurillo/gofastr/cmd/gofastr", External: true},
		}},
		{Title: "Make", Links: []sitefooter.Link{
			{Label: "Contribute", Href: "https://github.com/DonaldMurillo/gofastr/blob/main/CONTRIBUTING.md", External: true},
			{Label: "RFCs", Href: "https://github.com/DonaldMurillo/gofastr/tree/main/docs", External: true},
			{Label: "Releases", Href: "https://github.com/DonaldMurillo/gofastr/releases", External: true},
		}},
		{Title: "Elsewhere", Links: []sitefooter.Link{
			{Label: "GitHub", Href: "https://github.com/DonaldMurillo/gofastr", External: true},
			{Label: "pkg.go.dev", Href: "https://pkg.go.dev/github.com/DonaldMurillo/gofastr", External: true},
			{Label: "Discussions", Href: "https://github.com/DonaldMurillo/gofastr/discussions", External: true},
		}},
	}
}
