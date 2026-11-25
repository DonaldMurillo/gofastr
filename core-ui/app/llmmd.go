package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/urlprefix"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// ScreenLLMMDWithMeta renders the screen's llm.md after an optional caller-supplied metadata block.
func ScreenLLMMDWithMeta(ctx context.Context, screen *Screen, metaPrefix string) string {
	if metaPrefix == "" {
		return ScreenLLMMD(ctx, screen)
	}
	return metaPrefix + "\n" + ScreenLLMMD(ctx, screen)
}

// ScreenLLMMD generates an LLM-friendly markdown document for a single screen/page.
func ScreenLLMMD(ctx context.Context, screen *Screen) string {
	var b strings.Builder

	title := screen.Title
	if title == "" {
		title = screen.Path
	}
	writeScreenLLMMDHead(&b, screen, title, screen.Path)

	b.WriteString("---\n\n")
	b.WriteString("## Page Content\n\n")
	if screen.Component != nil {
		if _, ok := screen.Component.(ScreenLoader); ok {
			b.WriteString("_(Content is loaded dynamically via ScreenLoader, not available in static context. See the rendered page for full content.)_\n")
			return b.String()
		}

		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("llm.md: panic rendering screen %s: %s", screen.Path, textsafe.Recovered(r))
					b.WriteString("_(error rendering content: see server logs)_\n")
				}
			}()
			content, err := component.SafeRenderCtx(ctx, screen.Component)
			if err != nil {
				log.Printf("llm.md: render error for %s: %s", textsafe.ScrubControlBytes(screen.Path), textsafe.Recovered(err))
				b.WriteString("_(error rendering content: see server logs)_\n")
				return
			}
			htmlContent := string(localizeLinks(ctx, content))
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

func writeScreenLLMMDHead(b *strings.Builder, screen *Screen, title, path string) {
	fmt.Fprintf(b, "# %s\n\n", mdHeadField(title))

	b.WriteString("## Route\n\n")
	fmt.Fprintf(b, "- **Path:** `%s`\n", mdCodeSpan(path))
	if path != screen.Path {
		fmt.Fprintf(b, "- **Pattern:** `%s`\n", mdCodeSpan(screen.Path))
	}
	fmt.Fprintf(b, "- **Type:** %s\n", screen.Type.String())

	if screen.Description != "" {
		fmt.Fprintf(b, "- **Description:** %s\n", mdHeadField(screen.Description))
	}

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

// ScreenLLMMDWithheld renders a metadata-free llm.md for a screen whose policy chain returned non-Allow.
func ScreenLLMMDWithheld(screen *Screen) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", mdHeadField(screen.Path))
	b.WriteString("## Route\n\n")
	fmt.Fprintf(&b, "- **Path:** `%s`\n", mdCodeSpan(screen.Path))
	fmt.Fprintf(&b, "- **Type:** %s\n", screen.Type.String())
	b.WriteString("\n---\n\n")
	b.WriteString("## Page Content\n\n")
	b.WriteString("_(Content withheld: this route is access-controlled.)_\n")
	return b.String()
}

// ScreenLLMMDResult is what ScreenLLMMDForPath produced for a concrete URL.
type ScreenLLMMDResult struct {
	MD        string
	Title     string
	Allowed   bool
	Component component.Component
	Decision  Decision
	Err       error // A screen pipeline failure that prevented rendering the document.
}

// ScreenLLMMDForPath generates the per-screen llm.md for a CONCRETE URL of a (possibly dynamic) route.
func ScreenLLMMDForPath(ctx context.Context, a *App, path string) (ScreenLLMMDResult, bool) {
	screen, params, found := a.Router.Resolve(path)
	if !found {
		return ScreenLLMMDResult{}, false
	}

	if m, ok := MatchFromContext(ctx); !ok || m.path != decodeRouteSeg(path) {
		ctx = WithMatch(ctx, newMatch(screen.Path, path, params))
	}
	var chain []LayoutLayer
	if screen.Type == ScreenPage {
		chain = resolveChainKeys(a.Router.layoutChainFor(screen), params)
	}

	ctx, phase := runPolicyPhase(ctx, screen, path, chain)
	if errors.Is(phase.err, ErrNotFound) {
		return ScreenLLMMDResult{}, false
	}
	if phase.err != nil {
		return ScreenLLMMDResult{Err: phase.err}, true
	}
	if phase.decision.Kind != DecisionAllow {
		return ScreenLLMMDResult{MD: ScreenLLMMDWithheld(screen), Decision: phase.decision}, true
	}
	guards, guardRedirect := phase.guards, phase.redirect
	if guardRedirect.Kind == DecisionRedirect {
		return ScreenLLMMDResult{MD: ScreenLLMMDWithheld(screen), Decision: guardRedirect}, true
	}
	comp := screen.newInstance()
	if len(params) > 0 {
		if ps, ok := comp.(ParamSetter); ok {
			ps.SetParams(params)
		}
	}
	if err := a.injectComponent(comp, path); err != nil {
		return ScreenLLMMDResult{Err: err}, true
	}
	ctx = withResolvePhase(ctx, resolvePhaseLoad)
	var loadErr error
	if loader, ok := comp.(ScreenLoader); ok {
		loadErr = safeScreenLoad(loader, ctx)
	}
	if pe := resolverPageError(ctx, path); pe != nil {
		if errors.Is(pe, ErrNotFound) {
			return ScreenLLMMDResult{}, false
		}
		return ScreenLLMMDResult{Err: pe}, true
	}
	if errors.Is(loadErr, ErrNotFound) {
		return ScreenLLMMDResult{}, false
	}
	if loadErr != nil {
		return ScreenLLMMDResult{Err: fmt.Errorf("app: load failed for %q: %w", path, loadErr)}, true
	}

	title := screen.Title
	if t, ok := comp.(ScreenTitler); ok {
		if tt := safeScreenTitle(ctx, t); tt != "" {
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
	ctx = withResolvePhase(ctx, resolvePhaseRegion)
	content, err := renderScreen(ctx, comp)
	if err != nil {
		log.Printf("llm.md: render error for %s: %s", textsafe.ScrubControlBytes(path), textsafe.Recovered(err))
		return ScreenLLMMDResult{Err: screenRenderPanicError(path, err)}, true
	}
	if fills := a.resolveFills(ctx, path, params, screen, chain, false, guards); fills != nil && fills.notFoundOutlet {
		return ScreenLLMMDResult{}, false
	}
	if m := htmlToMarkdown(string(localizeLinks(ctx, content))); m != "" {
		b.WriteString(m)
		b.WriteString("\n")
	} else {
		b.WriteString("_(Screen renders no static content.)_\n")
	}
	return ScreenLLMMDResult{MD: b.String(), Title: title, Allowed: true, Component: comp}, true
}

func localizeLinks(ctx context.Context, content render.HTML) render.HTML {
	prefix := i18n.PathPrefix(ctx)
	if prefix == "" {
		return content
	}
	set, _, _ := i18n.PathLocale(ctx)
	rule := urlprefix.Rule{Prefix: prefix, Locales: set.All()}
	rule.Keep, _ = ctx.Value(linkKeepKey{}).(func(string) bool)
	if r := RequestFromContext(ctx); r != nil && r.Host != "" {
		rule.Origins = []string{"http://" + r.Host, "https://" + r.Host}
	}
	return render.HTML(urlprefix.HTML([]byte(content), rule))
}

type linkKeepKey struct{}

// WithLinkKeep returns ctx carrying keep, the urlprefix.Rule Keep the markdown docs' locale link
// rewrite asks: a link whose path it claims keeps its unprefixed form.
func WithLinkKeep(ctx context.Context, keep func(path string) bool) context.Context {
	return context.WithValue(ctx, linkKeepKey{}, keep)
}

type exportedDocsKey struct{}

// WithExportedDocs returns ctx for a static export's page index: docs maps each route pattern to
// the pages, in routing form and without a locale prefix, whose llm.md the export wrote.
func WithExportedDocs(ctx context.Context, docs map[string][]string) context.Context {
	return context.WithValue(ctx, exportedDocsKey{}, docs)
}

func rowLLMMDLink(ctx context.Context, path string) string {
	if docs, ok := ctx.Value(exportedDocsKey{}).(map[string][]string); ok && !slices.Contains(docs[path], path) {
		return ""
	}
	return pageLLMMDLink(ctx, path)
}

func dynamicPatternCell(ctx context.Context, pattern string) string {
	docs, ok := ctx.Value(exportedDocsKey{}).(map[string][]string)
	if !ok {
		return fmt.Sprintf("[%s](%s)", mdLinkText(pattern), mdLinkDest(pageLLMMDLink(ctx, pattern)))
	}
	cell := mdTableCell(pattern)
	for i, p := range docs[pattern] {
		sep := ", "
		if i == 0 {
			sep = ": "
		}
		cell += fmt.Sprintf("%s[%s](%s)", sep, mdLinkText(p), mdLinkDest(pageLLMMDLink(ctx, EscapeRoutePath(p))))
	}
	return cell
}

func pageLLMMDLink(ctx context.Context, path string) string {
	pre := i18n.PathPrefix(ctx)
	clean := strings.TrimRight(path, "/")
	if clean == "" {
		return pre + "/llm.md"
	}
	return pre + clean + "/llm.md"
}

// AppLLMMD generates a top-level LLM-friendly markdown index listing all screens/pages registered in the app.
func AppLLMMD(a *App) string {
	return AppLLMMDCtx(context.Background(), a)
}

// AppLLMMDCtx is AppLLMMD with each screen's policy evaluated; a refused screen is listed by path only.
func AppLLMMDCtx(ctx context.Context, a *App) string {
	var b strings.Builder

	title := a.Name
	if title == "" {
		title = "Application"
	}
	fmt.Fprintf(&b, "# %s: Page Reference\n\n", mdHeadField(title))
	b.WriteString("Auto-generated LLM-friendly documentation for all registered pages and screens.\n\n")
	b.WriteString("For API endpoints (CRUD resources), see [/api/llm.md](/api/llm.md).\n\n")

	routes := a.Routes()
	if len(routes) == 0 {
		b.WriteString("No pages registered.\n")
		return b.String()
	}

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
		if d := a.PatternPolicy(ctx, screen); d.notFound() {
			continue
		} else if d.Kind != DecisionAllow {
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
			link := rowLLMMDLink(ctx, r.Path)
			if link != "" {
				fmt.Fprintf(&b, "| [%s](%s) | %s | %s |\n", mdLinkText(r.Path), mdLinkDest(link), mdTableCell(r.Title), mdTableCell(desc))
			} else {
				fmt.Fprintf(&b, "| %s | %s | %s |\n", mdTableCell(r.Path), mdTableCell(r.Title), mdTableCell(desc))
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
			fmt.Fprintf(&b, "| %s | %s | `%s` | %s |\n", dynamicPatternCell(ctx, r.Path), mdTableCell(r.Title), mdCodeSpan(paramStr), mdTableCell(desc))
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
			if link := rowLLMMDLink(ctx, r.Path); link != "" {
				fmt.Fprintf(&b, "| [%s](%s) | %s | %s | %s |\n", mdLinkText(r.Path), mdLinkDest(link), mdTableCell(r.Title), screen.Type.String(), mdTableCell(desc))
			} else {
				fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", mdTableCell(r.Path), mdTableCell(r.Title), screen.Type.String(), mdTableCell(desc))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Architecture\n\n")
	b.WriteString("This application uses server-side rendering (SSR) with client-side hydration:\n\n")
	b.WriteString("1. **Initial load:** Every page is fully server-rendered as HTML.\n")
	b.WriteString("2. **Hydration:** `runtime.js` attaches event handlers to the existing DOM after first paint.\n")
	b.WriteString("3. **Cross-page navigation:** Client-side fetch with content swap, no full page reload.\n")
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

// ScreenLLMMDHandler returns an http.Handler that serves the LLM-friendly markdown for a single screen of a.
func ScreenLLMMDHandler(a *App, screen *Screen) http.Handler {
	md := ScreenLLMMD(context.Background(), screen)
	withheld := ScreenLLMMDWithheld(screen)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithRequest(r.Context(), r)
		body := md
		if d := a.PatternPolicy(ctx, screen); d.notFound() {
			http.NotFound(w, r)
			return
		} else if d.Kind != DecisionAllow {
			body = withheld
		} else if i18n.PathPrefix(ctx) != "" || i18n.TranslatorFrom(ctx) != nil || i18n.FromContext(ctx).Tag != "" {
			body = ScreenLLMMD(ctx, screen)
		}
		setMarkdownHeaders(w)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write([]byte(body))
	})
}

// AppLLMMDHandler returns an http.Handler that serves the top-level page index markdown for all
// screens in the app.
func AppLLMMDHandler(a *App) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		md := AppLLMMDCtx(WithRequest(r.Context(), r), a)
		setMarkdownHeaders(w)
		w.Header().Set("Content-Length", strconv.Itoa(len(md)))
		w.Write([]byte(md))
	})
}

func setMarkdownHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	writer.Header().Set("Cache-Control", "private, no-store")
	writer.Header().Add("Vary", "Cookie")
}

func extractParamNames(path string) []string {
	var names []string
	for seg := range strings.SplitSeq(strings.Trim(path, "/"), "/") {
		if isParamSeg(seg) {
			names = append(names, segParamName(seg))
		}
	}
	return names
}
