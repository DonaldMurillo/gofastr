// =============================================================================
// /plugins and /plugins/<name>: the gofastr-plugins registry, rendered.
//
// The sibling repo publishes plugins.json with every release. It is
// consumed by COPY, never by import: importing a Go API for it would make
// gofastr depend on a repo that depends on gofastr. So a vendored copy
// lives at plugins/plugins.json, refreshed by scripts/vendor-plugins-json.sh
// from a release asset, and these screens render one index plus one page
// per plugin from it. The release stamp on the copy (tag, commit,
// published) is shown on the page so a stale copy is visibly stale.
// screen_plugins_test.go gates the copy's shape.
// =============================================================================

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

//go:embed plugins/plugins.json
var pluginRegistryJSON []byte

// pluginRegistry mirrors the published plugins.json shape (registryVersion
// "1"). Parsing rejects unknown fields on purpose: a key the registry adds
// must reach these structs, and the page, in the same change as the refresh.
type pluginRegistry struct {
	RegistryVersion string          `json:"registryVersion"`
	Comment         string          `json:"$comment,omitempty"`
	Description     string          `json:"description"`
	Framework       pluginFramework `json:"framework"`
	Plugins         []pluginEntry   `json:"plugins"`
	// Release is stamped by the gofastr-plugins release workflow. The file in
	// that repo's git has none, so its presence proves the copy came from a
	// release rather than a hand copy.
	Release *pluginRelease `json:"release,omitempty"`
}

type pluginFramework struct {
	Name       string `json:"name"`
	ModulePath string `json:"modulePath"`
	Note       string `json:"note,omitempty"`
}

type pluginRelease struct {
	Tag       string `json:"tag"`
	Commit    string `json:"commit"`
	Published string `json:"published"`
	Source    string `json:"source"`
}

type pluginEntry struct {
	Name                 string   `json:"name"`
	ModulePath           string   `json:"modulePath"`
	Version              string   `json:"version"`
	Description          string   `json:"description"`
	Isolation            string   `json:"isolation"`
	Trusted              bool     `json:"trusted,omitempty"`
	Sandbox              []string `json:"sandbox,omitempty"`
	Capabilities         []string `json:"capabilities"`
	OptionalCapabilities []string `json:"optionalCapabilities,omitempty"`
	CSP                  []string `json:"csp,omitempty"`
	FrameworkCompat      string   `json:"frameworkCompat"`
	RoutePrefix          string   `json:"routePrefix"`
	Entry                string   `json:"entry"`
	Schema               string   `json:"schema"`
	MinHeight            string   `json:"minHeight"`
	Docs                 string   `json:"docs"`
}

// parsePluginRegistry decodes a registry copy strictly. Exposed for the
// gate test; the screens use the embedded copy through pluginReg.
func parsePluginRegistry(data []byte) (*pluginRegistry, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var reg pluginRegistry
	if err := dec.Decode(&reg); err != nil {
		return nil, fmt.Errorf("plugins.json: %w", err)
	}
	if reg.RegistryVersion != "1" {
		return nil, fmt.Errorf("plugins.json: registryVersion %q, this site renders version 1", reg.RegistryVersion)
	}
	if reg.Release == nil || reg.Release.Tag == "" {
		return nil, fmt.Errorf("plugins.json: no release stamp; vendor a published copy with scripts/vendor-plugins-json.sh")
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("plugins.json: trailing data after the registry object")
	}
	if err := validatePluginRegistry(&reg); err != nil {
		return nil, err
	}
	return &reg, nil
}

// validatePluginRegistry enforces what the decoder cannot: every row is
// complete, names are unique, the isolation value is one the pages know
// how to present, and the trusted/sandbox pairing matches that value. An
// unknown isolation would otherwise render as a sandboxed iframe, the
// safer-looking posture, which is the wrong way for a mistake to fail.
func validatePluginRegistry(reg *pluginRegistry) error {
	if len(reg.Plugins) == 0 {
		return fmt.Errorf("plugins.json: no plugins")
	}
	if reg.Release.Commit == "" || reg.Release.Published == "" || !strings.HasPrefix(reg.Release.Source, "https://") {
		return fmt.Errorf("plugins.json: release stamp incomplete: %+v", *reg.Release)
	}
	seen := make(map[string]bool, len(reg.Plugins))
	root := ""
	for _, p := range reg.Plugins {
		if seen[p.Name] {
			return fmt.Errorf("plugins.json: duplicate row %q", p.Name)
		}
		seen[p.Name] = true
		for k, v := range map[string]string{"name": p.Name, "modulePath": p.ModulePath, "version": p.Version, "description": p.Description, "frameworkCompat": p.FrameworkCompat, "routePrefix": p.RoutePrefix} {
			if v == "" {
				return fmt.Errorf("plugins.json: row %q has an empty %s", p.Name, k)
			}
		}
		if root == "" {
			root = pluginRootModule(p.ModulePath)
		} else if got := pluginRootModule(p.ModulePath); got != root {
			return fmt.Errorf("plugins.json: row %q lives in module %q, the rest in %q", p.Name, got, root)
		}
		switch p.Isolation {
		case "sandbox-iframe-opaque":
			if p.Trusted {
				return fmt.Errorf("plugins.json: sandboxed row %q is marked trusted", p.Name)
			}
			for _, tok := range p.Sandbox {
				if tok == "allow-same-origin" {
					return fmt.Errorf("plugins.json: row %q grants allow-same-origin, which collapses the opaque origin", p.Name)
				}
			}
		case "trusted-host-page":
			if !p.Trusted {
				return fmt.Errorf("plugins.json: trusted-host-page row %q lacks trusted:true", p.Name)
			}
			if len(p.Sandbox) != 0 {
				return fmt.Errorf("plugins.json: trusted row %q declares sandbox tokens %v", p.Name, p.Sandbox)
			}
		default:
			return fmt.Errorf("plugins.json: row %q has unknown isolation %q", p.Name, p.Isolation)
		}
	}
	return nil
}

var (
	pluginRegOnce sync.Once
	pluginRegVal  *pluginRegistry
	pluginRegErr  error
)

// pluginReg returns the embedded registry, parsed once. A copy that fails
// the parse is a build-time mistake the gate test reports first; at
// runtime the screens fall back to an empty index instead of panicking.
func pluginReg() (*pluginRegistry, error) {
	pluginRegOnce.Do(func() { pluginRegVal, pluginRegErr = parsePluginRegistry(pluginRegistryJSON) })
	return pluginRegVal, pluginRegErr
}

func pluginByName(name string) (pluginEntry, bool) {
	reg, err := pluginReg()
	if err != nil {
		return pluginEntry{}, false
	}
	for _, p := range reg.Plugins {
		if p.Name == name {
			return p, true
		}
	}
	return pluginEntry{}, false
}

// pluginRootModule is the go-gettable module a plugin package lives in,
// the first three path segments of its module path.
func pluginRootModule(modulePath string) string {
	parts := strings.SplitN(modulePath, "/", 4)
	if len(parts) < 3 {
		return modulePath
	}
	return strings.Join(parts[:3], "/")
}

// pluginDocsURL points at the plugin's doc in the sibling repo at the
// vendored release's tag, so the link matches the version described.
func pluginDocsURL(reg *pluginRegistry, p pluginEntry) string {
	if p.Docs == "" || reg.Release == nil {
		return ""
	}
	return strings.TrimSuffix(reg.Release.Source, "/") + "/blob/" + reg.Release.Tag + "/" + p.Docs
}

func pluginIsolationPill(p pluginEntry) render.HTML {
	if p.Isolation == "trusted-host-page" {
		return ui.StatusPill(ui.StatusPillConfig{Label: "trusted host page", Dot: true})
	}
	return ui.StatusPill(ui.StatusPillConfig{Label: "sandboxed iframe", Tone: ui.StatusPillAccent, Dot: true})
}

// pluginSummary is the card-length description: the registry's descriptions
// run from one sentence to a paragraph, so the index shows the first
// sentence and the plugin's own page carries the whole text.
func pluginSummary(desc string) string {
	const cap = 180
	if i := strings.Index(desc, ". "); i > 0 && i+1 <= cap {
		return desc[:i+1]
	}
	if len(desc) <= cap {
		return desc
	}
	cut := strings.LastIndex(desc[:cap], " ")
	if cut <= 0 {
		cut = cap
	}
	return desc[:cut] + "…"
}

func pluginPills(p pluginEntry) render.HTML {
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM},
		ui.StatusPill(ui.StatusPillConfig{Label: "v" + p.Version}),
		pluginIsolationPill(p),
	)
}

// =============================================================================
// /plugins
// =============================================================================

type PluginsScreen struct{}

func (s *PluginsScreen) ScreenTitle() string { return "Plugins" }
func (s *PluginsScreen) ScreenDescription() string {
	reg, err := pluginReg()
	if err != nil {
		return "Officially maintained heavy-JavaScript plugins for GoFastr."
	}
	return fmt.Sprintf("%d officially maintained heavy-JavaScript plugins for GoFastr, from the gofastr-plugins registry at %s. Each is one go get away and runs isolated from the host by default.", len(reg.Plugins), reg.Release.Tag)
}
func (s *PluginsScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *PluginsScreen) Render() render.HTML {
	reg, err := pluginReg()
	if err != nil {
		return container(ui.Callout(ui.CalloutConfig{Title: "Registry unavailable", Variant: ui.StatusWarning},
			render.Text("The vendored plugins.json did not parse: "+err.Error())))
	}
	cards := make([]render.HTML, 0, len(reg.Plugins))
	for _, p := range reg.Plugins {
		cards = append(cards, ui.Card(ui.CardConfig{
			Heading:      p.Name,
			HeadingLevel: 2,
			Description:  pluginSummary(p.Description),
			Href:         "/plugins/" + p.Name,
			Footer:       pluginPills(p),
		}))
	}
	return container(
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "gofastr-plugins · " + reg.Release.Tag,
			Title:    fmt.Sprintf("%d plugins, each one go get away", len(reg.Plugins)),
			Subtitle: "The heavy-JavaScript features that would break the core's runtime budget live in a sibling repo. Most run in an opaque-origin sandboxed iframe and reach the host only over a versioned postMessage bridge; the few that must touch the host page say so.",
		}),
		ui.Grid(ui.GridConfig{Min: "20rem", Gap: ui.GapLG}, cards...),
		ui.Section(ui.SectionConfig{
			ID:      "registry",
			Heading: "Where this list comes from",
			Description: fmt.Sprintf("This page is rendered from plugins.json as published with gofastr-plugins %s on %s. The registry is a convention, not a service: there is no discovery at runtime, an app imports the package it wants and mounts it with app.RegisterPlugin. The site vendors the file by copy and refreshes it with scripts/vendor-plugins-json.sh.",
				reg.Release.Tag, strings.SplitN(reg.Release.Published, "T", 2)[0]),
		},
			ui.CodeBlock(ui.CodeBlockConfig{
				Language: "bash",
				Code:     "curl -fsSL -O " + strings.TrimSuffix(reg.Release.Source, "/") + "/releases/download/" + reg.Release.Tag + "/plugins.json",
			}),
			html.Paragraph(html.TextConfig{},
				render.Text("The isolation model, the capability grammar, and the trusted-mount opt-out are in "),
				html.Link(html.LinkConfig{Href: "/docs/plugin-platform", Text: "the plugin platform doc"}),
				render.Text(". The registry file itself is on "),
				html.LinkHTML(html.LinkHTMLConfig{Href: reg.Release.Source, ExtraAttrs: html.Attrs{"rel": "external"}, Content: render.Text("GitHub")}),
				render.Text("."),
			),
		),
	)
}

// =============================================================================
// /plugins/<name>
// =============================================================================

type PluginScreen struct{ name string }

func (s *PluginScreen) SetParams(p map[string]string) { s.name = p["name"] }
func (s *PluginScreen) ScreenType() app.ScreenType    { return app.ScreenPage }

func (s *PluginScreen) ScreenTitle() string {
	if p, ok := pluginByName(s.name); ok {
		return p.Name + " plugin"
	}
	return "Plugin"
}

func (s *PluginScreen) ScreenDescription() string {
	if p, ok := pluginByName(s.name); ok {
		return p.Description
	}
	return "A gofastr-plugins entry."
}

// Load rejects unknown names so the site serves its 404 instead of an
// empty plugin page.
func (s *PluginScreen) Load(ctx context.Context) error {
	if _, ok := pluginByName(s.name); !ok {
		return fmt.Errorf("plugins: no registry entry for %q", s.name)
	}
	return nil
}

// StaticPaths enumerates every plugin so the static export, sitemap, and
// strict coverage gate produce one page per registry row.
func (s *PluginScreen) StaticPaths(ctx context.Context) []map[string]string {
	reg, err := pluginReg()
	if err != nil {
		return nil
	}
	out := make([]map[string]string, 0, len(reg.Plugins))
	for _, p := range reg.Plugins {
		out = append(out, map[string]string{"name": p.Name})
	}
	return out
}

func (s *PluginScreen) Render() render.HTML {
	reg, err := pluginReg()
	p, ok := pluginByName(s.name)
	if err != nil || !ok {
		return container(ui.EmptyState(ui.EmptyStateConfig{
			Title:        "No such plugin",
			Description:  "That name is not in the vendored registry.",
			HeadingLevel: 1,
		}), html.Link(html.LinkConfig{Href: "/plugins", Text: "Back to the plugin list"}))
	}
	pkg := path.Base(p.ModulePath)
	mount := fmt.Sprintf("go get %s@%s\n\nimport %q\n\napp.RegisterPlugin(%s.New())", pluginRootModule(p.ModulePath), reg.Release.Tag, p.ModulePath, pkg)

	list := func(v []string) render.HTML {
		if len(v) == 0 {
			return html.Span(html.TextConfig{}, render.Text("none"))
		}
		return render.Text(strings.Join(v, ", "))
	}
	items := []ui.DetailItem{
		{Label: "Module path", Value: html.LinkHTML(html.LinkHTMLConfig{
			Href: "https://pkg.go.dev/" + p.ModulePath, ExtraAttrs: html.Attrs{"rel": "external"},
			Content: render.Text(p.ModulePath)})},
		{Label: "Plugin version", Value: render.Text(p.Version)},
		{Label: "Framework compat", Value: render.Text(p.FrameworkCompat)},
		{Label: "Isolation", Value: render.Text(p.Isolation)},
		{Label: "Sandbox tokens", Value: list(p.Sandbox)},
		{Label: "Capabilities", Value: list(p.Capabilities)},
		{Label: "Optional capabilities", Value: list(p.OptionalCapabilities)},
		{Label: "CSP additions", Value: list(p.CSP)},
		{Label: "Route prefix", Value: codeText(p.RoutePrefix)},
		{Label: "Entry", Value: codeText(p.Entry)},
		{Label: "Schema", Value: render.Text(p.Schema)},
		{Label: "Min height", Value: render.Text(p.MinHeight)},
	}
	if u := pluginDocsURL(reg, p); u != "" {
		items = append(items, ui.DetailItem{Label: "Docs", Value: html.LinkHTML(html.LinkHTMLConfig{
			Href: u, ExtraAttrs: html.Attrs{"rel": "external"}, Content: render.Text(p.Docs)})})
	}

	var posture render.HTML
	if p.Isolation == "trusted-host-page" {
		posture = ui.Callout(ui.CalloutConfig{Title: "Runs in the host page", Variant: ui.StatusWarning},
			render.Text("This plugin is not sandboxed. It has full DOM access because its job needs it, and the app owner who compiles it in vouches for it. Its capability names still gate its server endpoints."))
	} else {
		posture = ui.Callout(ui.CalloutConfig{Title: "Runs in an opaque-origin iframe", Variant: ui.StatusInfo},
			render.Text("The frame never gets allow-same-origin, so it cannot reach host cookies, the database, or the host DOM. It talks to the host only over the versioned postMessage bridge, limited to the capabilities listed below."))
	}

	return container(
		ui.PageHeader(ui.PageHeaderConfig{
			Eyebrow:  "gofastr-plugins · " + reg.Release.Tag,
			Title:    p.Name,
			Subtitle: p.Description,
			Actions:  pluginPills(p),
		}),
		ui.Section(ui.SectionConfig{ID: "install", Heading: "Install and mount"},
			ui.CodeBlock(ui.CodeBlockConfig{Language: "go", Code: mount, ShowCopy: true}),
		),
		ui.Section(ui.SectionConfig{ID: "posture", Heading: "Isolation"}, posture),
		ui.Section(ui.SectionConfig{ID: "manifest", Heading: "Manifest"},
			ui.DetailList(ui.DetailListConfig{Items: items}),
		),
		html.Paragraph(html.TextConfig{}, html.Link(html.LinkConfig{Href: "/plugins", Text: "All plugins"})),
	)
}
