package app

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

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
	fmt.Fprintf(&b, "# %s\n\n", title)

	// Route info (compact summary)
	b.WriteString("## Route\n\n")
	fmt.Fprintf(&b, "- **Path:** `%s`\n", screen.Path)
	fmt.Fprintf(&b, "- **Type:** %s\n", screen.Type.String())

	if screen.Description != "" {
		fmt.Fprintf(&b, "- **Description:** %s\n", screen.Description)
	}

	// Dynamic params
	params := extractParamNames(screen.Path)
	if len(params) > 0 {
		b.WriteString("- **Params:** ")
		for i, p := range params {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "`%s`", p)
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
		fmt.Fprintf(&b, "- **Capabilities:** %s\n", strings.Join(capabilities, ", "))
	}
	b.WriteString("\n")

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
		screen, _, ok := a.Router.Resolve(r.Path)
		if !ok {
			continue
		}
		if screen.NoLLMMD {
			continue
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
			screen, _, ok := a.Router.Resolve(r.Path)
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
// markdown for a single screen. Content-Type is text/markdown.
func ScreenLLMMDHandler(screen *Screen) http.Handler {
	md := ScreenLLMMD(screen)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Length", strconv.Itoa(len(md)))
		w.Write([]byte(md))
	})
}

// AppLLMMDHandler returns an http.Handler that serves the top-level
// page index markdown for all screens in the app.
func AppLLMMDHandler(a *App) http.Handler {
	md := AppLLMMD(a)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		if strings.HasPrefix(seg, ":") {
			names = append(names, seg[1:])
		}
	}
	return names
}
