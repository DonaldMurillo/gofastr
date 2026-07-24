package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// ScreenLLMMDWithMeta renders the screen's llm.md with an optional
// caller-supplied metadata block (e.g. SEO front-matter resolved by an
// upper layer that can see the screen's SEO interfaces) inserted at the
// very top of the document. metaPrefix is emitted verbatim before the
// "# <title>" heading; pass "" for no prefix.
//
// The layering contract: core-ui/app cannot import framework/uihost,
// so the SEO bundle is resolved in the host layer and passed down here
// as an opaque prefix string. This keeps the markdown builder free of
// the SEO type graph while still inheriting every field the HTML head
// emits.
func ScreenLLMMDWithMeta(screen *Screen, metaPrefix string) string {
	if metaPrefix == "" {
		return ScreenLLMMD(screen)
	}
	return metaPrefix + "\n" + ScreenLLMMD(screen)
}

// ScreenLLMMD generates an LLM-friendly markdown document for a single
// screen/page. The output describes the route, its parameters, the
// screen type, and any metadata useful for LLM agents navigating the app.
//
// When a screen implements ScreenDescriber, the description is included.
// When it implements StaticPathsProvider, the SSG behavior is documented.
// When it implements ScreenActions, the server-action contract is noted.
func ScreenLLMMD(screen *Screen) string {
	var b strings.Builder

	// Title
	title := screen.Title
	if title == "" {
		title = screen.Path
	}
	writeScreenLLMMDHead(&b, screen, title, screen.Path)

	// Page content — render the screen and convert HTML → markdown
	b.WriteString("---\n\n")
	b.WriteString("## Page Content\n\n")
	if screen.Component != nil {
		// ScreenLoader screens load data dynamically at request time.
		// Render() without Load() produces placeholder content.
		if _, ok := screen.Component.(ScreenLoader); ok {
			b.WriteString("_(Content is loaded dynamically via ScreenLoader — not available in static context. See the rendered page for full content.)_\n")
			return b.String()
		}

		// Guard against panics in Render()
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("llm.md: panic rendering screen %s: %v", screen.Path, r)
					b.WriteString("_(error rendering content — see server logs)_\n")
				}
			}()
			htmlContent := string(screen.Component.Render())
			md := htmlToMarkdown(htmlContent)
			if md != "" {
				b.WriteString(md)
				b.WriteString("\n")
			} else {
				b.WriteString("_(Screen renders no static content.)_\n")
			}
		}()
	}

	return b.String()
}

// writeScreenLLMMDHead writes the title + route-info header shared by the
// pattern-level doc (ScreenLLMMD) and the concrete-URL doc
// (ScreenLLMMDForPath). path is the path to display — the registered
// pattern for the former, the concrete URL for the latter (with the
// pattern shown alongside when they differ).
func writeScreenLLMMDHead(b *strings.Builder, screen *Screen, title, path string) {
	fmt.Fprintf(b, "# %s\n\n", title)

	// Route info (compact summary)
	b.WriteString("## Route\n\n")
	fmt.Fprintf(b, "- **Path:** `%s`\n", path)
	if path != screen.Path {
		fmt.Fprintf(b, "- **Pattern:** `%s`\n", screen.Path)
	}
	fmt.Fprintf(b, "- **Type:** %s\n", screen.Type.String())

	if screen.Description != "" {
		fmt.Fprintf(b, "- **Description:** %s\n", screen.Description)
	}

	// Dynamic params
	params := extractParamNames(screen.Path)
	if len(params) > 0 {
		b.WriteString("- **Params:** ")
		for i, p := range params {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "`%s`", p)
		}
		b.WriteString("\n")
	}

	// Screen capabilities (compact)
	var capabilities []string
	if _, ok := screen.Component.(ScreenLoader); ok {
		capabilities = append(capabilities, "ScreenLoader")
	}
	if _, ok := screen.Component.(StaticPathsProvider); ok {
		capabilities = append(capabilities, "StaticPathsProvider")
	}
	if _, ok := screen.Component.(ScreenActions); ok {
		capabilities = append(capabilities, "ScreenActions")
	}
	if _, ok := screen.Component.(ParamSetter); ok {
		capabilities = append(capabilities, "ParamSetter")
	}
	if len(capabilities) > 0 {
		fmt.Fprintf(b, "- **Capabilities:** %s\n", strings.Join(capabilities, ", "))
	}
	b.WriteString("\n")
}

// ScreenLLMMDWithheld renders a metadata-free llm.md for a screen whose
// policy chain returned non-Allow. ONLY the route's path, pattern, and
// type appear — the title, description, SEO bundle, and content are all
// component-supplied and therefore protected by the same policy that
// gates the page render (a screen titled "Project X" must not leak the
// name through its doc). The route's existence and shape are
// documentation; everything else is not.
func ScreenLLMMDWithheld(screen *Screen) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", screen.Path)
	b.WriteString("## Route\n\n")
	fmt.Fprintf(&b, "- **Path:** `%s`\n", screen.Path)
	fmt.Fprintf(&b, "- **Type:** %s\n", screen.Type.String())
	b.WriteString("\n---\n\n")
	b.WriteString("## Page Content\n\n")
	b.WriteString("_(Content withheld — this route is access-controlled.)_\n")
	return b.String()
}

// ScreenLLMMDResult is what ScreenLLMMDForPath produced for a concrete
// URL. Allowed reports whether the policy chain returned Allow — when
// false, MD is the metadata-free withheld doc and callers MUST NOT
// attach any component-derived metadata (titles, SEO front matter) of
// their own. Component is the loaded per-request instance when Allowed
// (nil otherwise), for hosts that resolve per-instance SEO.
type ScreenLLMMDResult struct {
	MD        string
	Title     string
	Allowed   bool
	Component component.Component
}

// ScreenLLMMDForPath generates the per-screen llm.md for a CONCRETE URL
// of a (possibly dynamic) route. Unlike ScreenLLMMD — which documents the
// registered pattern and emits a placeholder for ScreenLoader screens —
// this builds the same per-request instance the page render uses
// (SetParams → DI → Load) so the document carries the loaded title and
// rendered content. ok is false when the path resolves to no screen; a
// DI or Load failure falls back to the pattern-level doc rather than
// erroring (llm.md is documentation — a degraded doc beats a 500).
func ScreenLLMMDForPath(ctx context.Context, a *App, path string) (ScreenLLMMDResult, bool) {
	screen, params, found := a.Router.Resolve(path)
	if !found {
		return ScreenLLMMDResult{}, false
	}

	// Evaluate the policy chain BEFORE building the instance — this
	// function runs Load, so skipping the chain would leak loaded content
	// a Redirect/Block policy protects on the page render (an authed
	// detail screen must not answer via its /llm.md sibling). Anything
	// but a plain Allow degrades to the metadata-free withheld doc: the
	// pattern doc is NOT safe here — its title/description/content are
	// component-supplied, which the policy protects too.
	if ResolvePolicy(ctx, screen).Kind != DecisionAllow {
		return ScreenLLMMDResult{MD: ScreenLLMMDWithheld(screen)}, true
	}

	comp := screen.newInstance()
	if len(params) > 0 {
		if ps, ok := comp.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}
	loaded := true
	if err := a.Inject(comp); err != nil {
		loaded = false
	}
	if loaded {
		if loader, ok := comp.(ScreenLoader); ok {
			if err := loader.Load(ctx); err != nil {
				loaded = false
			}
		}
	}
	if !loaded {
		return ScreenLLMMDResult{MD: ScreenLLMMD(screen), Title: screen.Title, Allowed: true}, true
	}

	title := screen.Title
	if t, ok := comp.(ScreenTitler); ok {
		if tt := t.ScreenTitle(); tt != "" {
			title = tt
		}
	}
	headTitle := title
	if headTitle == "" {
		headTitle = path
	}

	var b strings.Builder
	writeScreenLLMMDHead(&b, screen, headTitle, path)

	b.WriteString("---\n\n")
	b.WriteString("## Page Content\n\n")
	content, err := component.SafeRenderCtx(ctx, comp)
	if err != nil {
		log.Printf("llm.md: render error for %s: %v", path, err)
		b.WriteString("_(error rendering content — see server logs)_\n")
		return ScreenLLMMDResult{MD: b.String(), Title: title, Allowed: true, Component: comp}, true
	}
	if m := htmlToMarkdown(string(content)); m != "" {
		b.WriteString(m)
		b.WriteString("\n")
	} else {
		b.WriteString("_(Screen renders no static content.)_\n")
	}
	return ScreenLLMMDResult{MD: b.String(), Title: title, Allowed: true, Component: comp}, true
}

// AppLLMMD generates a top-level LLM-friendly markdown index listing all
// screens/pages registered in the app. Each entry links to its per-page
// llm.md endpoint.
// pageLLMMDLink returns the llm.md link for a route path, or empty
// string if the route has no per-page llm.md.
func pageLLMMDLink(path string) string {
	clean := strings.TrimRight(path, "/")
	if clean == "" {
		// Root "/" — its llm.md is at /llm.md (entity index moved to /api/llm.md)
		return "/llm.md"
	}
	return clean + "/llm.md"
}

func AppLLMMD(a *App) string {
	return AppLLMMDCtx(context.Background(), a)
}

// AppLLMMDCtx is AppLLMMD with a request context: each screen's policy
// chain is evaluated and non-Allow screens are listed with their PATH
// ONLY — the title and description are component-supplied and protected
// by the same policy that gates the page render. Hosts pass the live
// request context so an authenticated agent sees the full index; the
// background-context AppLLMMD (and the static export) fail closed for
// gated screens.
func AppLLMMDCtx(ctx context.Context, a *App) string {
	var b strings.Builder

	title := a.Name
	if title == "" {
		title = "Application"
	}
	fmt.Fprintf(&b, "# %s — Page Reference\n\n", title)
	b.WriteString("Auto-generated LLM-friendly documentation for all registered pages and screens.\n\n")
	b.WriteString("For API endpoints (CRUD resources), see [/api/llm.md](/api/llm.md).\n\n")

	routes := a.Routes()
	if len(routes) == 0 {
		b.WriteString("No pages registered.\n")
		return b.String()
	}

	// Group by type
	pages := make([]RouteEntry, 0)
	overlays := make([]RouteEntry, 0)
	dynamic := make([]RouteEntry, 0)

	for _, r := range routes {
		if r.RedirectTo != "" {
			continue // redirects render no page
		}
		screen, ok := a.Router.ScreenByPattern(r.Path)
		if !ok {
			continue
		}
		if screen.NoLLMMD {
			continue
		}
		// Policy-gated screens stay listed (existence and shape are
		// documentation) but metadata-free.
		if ResolvePolicy(ctx, screen).Kind != DecisionAllow {
			r.Title, r.Description = "", ""
		}
		if strings.Contains(r.Path, ":") {
			dynamic = append(dynamic, r)
		} else if screen.Type == ScreenPage {
			pages = append(pages, r)
		} else {
			overlays = append(overlays, r)
		}
	}

	if len(pages) > 0 {
		b.WriteString("## Pages\n\n")
		b.WriteString("| Path | Title | Description |\n")
		b.WriteString("|------|-------|-------------|\n")
		for _, r := range pages {
			desc := r.Description
			if desc == "" {
				desc = "—"
			}
			link := pageLLMMDLink(r.Path)
			if link != "" {
				fmt.Fprintf(&b, "| [%s](%s) | %s | %s |\n", r.Path, link, r.Title, desc)
			} else {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", r.Path, r.Title, desc)
			}
		}
		b.WriteString("\n")
	}

	if len(dynamic) > 0 {
		b.WriteString("## Dynamic Routes\n\n")
		b.WriteString("| Pattern | Title | Params | Description |\n")
		b.WriteString("|---------|-------|--------|-------------|\n")
		for _, r := range dynamic {
			params := extractParamNames(r.Path)
			paramStr := strings.Join(params, ", ")
			desc := r.Description
			if desc == "" {
				desc = "—"
			}
			link := pageLLMMDLink(r.Path)
			if link != "" {
				fmt.Fprintf(&b, "| [%s](%s) | %s | `%s` | %s |\n", r.Path, link, r.Title, paramStr, desc)
			} else {
				fmt.Fprintf(&b, "| %s | %s | `%s` | %s |\n", r.Path, r.Title, paramStr, desc)
			}
		}
		b.WriteString("\n")
	}

	if len(overlays) > 0 {
		b.WriteString("## Overlays (Drawers / Sheets / Dialogs)\n\n")
		b.WriteString("| Path | Title | Type | Description |\n")
		b.WriteString("|------|-------|------|-------------|\n")
		for _, r := range overlays {
			screen, ok := a.Router.ScreenByPattern(r.Path)
			if !ok {
				continue
			}
			desc := r.Description
			if desc == "" {
				desc = "—"
			}
			fmt.Fprintf(&b, "| [%s](%s/llm.md) | %s | %s | %s |\n", r.Path, r.Path, r.Title, screen.Type.String(), desc)
		}
		b.WriteString("\n")
	}

	// Architecture notes for LLMs
	b.WriteString("## Architecture\n\n")
	b.WriteString("This application uses server-side rendering (SSR) with client-side hydration:\n\n")
	b.WriteString("1. **Initial load:** Every page is fully server-rendered as HTML.\n")
	b.WriteString("2. **Hydration:** `runtime.js` attaches event handlers to the existing DOM after first paint.\n")
	b.WriteString("3. **Cross-page navigation:** Client-side fetch with content swap — no full page reload.\n")
	b.WriteString("4. **In-page state:** Interactive regions (islands/widgets) update via RPC, not URL changes.\n")
	b.WriteString("5. **Server push:** Real-time updates flow through SSE for background events only.\n")
	b.WriteString("\n")
	b.WriteString("### Endpoints\n\n")
	b.WriteString("| Endpoint | Purpose |\n")
	b.WriteString("|----------|----------|\n")
	b.WriteString("| `/{path}` | Full SSR HTML page |\n")
	b.WriteString("| `/{path}/llm.md` | LLM-friendly documentation for that page |\n")
	b.WriteString("| `/llm-pages.md` | This index document |\n")
	b.WriteString("| `/__gofastr/runtime.js` | Client-side runtime (hydration, SPA nav, islands) |\n")
	b.WriteString("| `/__gofastr/app.css` | Merged theme + component CSS |\n")
	b.WriteString("| `/__gofastr/sse` | Server-Sent Events stream |\n")

	return b.String()
}

// ScreenLLMMDHandler returns an http.Handler that serves the LLM-friendly
// markdown for a single screen. Content-Type is text/markdown. The
// screen's policy chain is evaluated per request — a non-Allow decision
// serves the metadata-free withheld doc, matching every other llm.md
// surface.
func ScreenLLMMDHandler(screen *Screen) http.Handler {
	md := ScreenLLMMD(screen)
	withheld := ScreenLLMMDWithheld(screen)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := md
		if ResolvePolicy(WithRequest(r.Context(), r), screen).Kind != DecisionAllow {
			body = withheld
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write([]byte(body))
	})
}

// AppLLMMDHandler returns an http.Handler that serves the top-level
// page index markdown for all screens in the app. Policy-gated screens
// are listed metadata-free unless the request's policy evaluation
// allows them (see AppLLMMDCtx).
func AppLLMMDHandler(a *App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		md := AppLLMMDCtx(WithRequest(r.Context(), r), a)
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(md)))
		w.Write([]byte(md))
	})
}

// extractParamNames parses a route path like "/products/:slug/reviews/:id"
// and returns the parameter names ["slug", "id"].
func extractParamNames(path string) []string {
	var names []string
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if isParamSeg(seg) {
			names = append(names, segParamName(seg))
		}
	}
	return names
}
