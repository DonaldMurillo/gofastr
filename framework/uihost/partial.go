package uihost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/uihost/internal/sessiontoken"
)

// handlePartRequest answers one deferred outlet's part request
// (X-Gofastr-Part, the fill for ONE wire address
// of the resolved route, read by the client with text() so DevTools
// shows the request with its body.
//
// A part never mints a session (the page request beside it owns the
// rollover; a mint here would race two Set-Cookies onto one
// navigation), and always carries no-store. Every whole-page outcome —
// the route not resolving, the address not being a deferred outlet of
// it, the policy phase redirecting or blocking, a render error —
// answers 409 with X-Gofastr-Part-Reset: 1 and a body the client
// applies nothing from; the runtime then reloads the URL as a whole
// document, once per navigation. The one exception is a DEAD SESSION:
// that reset says `session`, and the runtime waits for the page
// request's commit (its Set-Cookie carries the re-minted token) and
// re-requests the part once before ever reloading.
func (ds *UIHost) handlePartRequest(w http.ResponseWriter, r *http.Request, path, addr string) {
	w.Header().Set("Cache-Control", "no-store")
	if staleMarkupClient(r) {
		ds.writePartReset(w, partResetAny)
		return
	}
	// A dead session between the page request and its parts is the
	// spec's reset case: the fill would render under a different
	// identity than the page beside it. The reset names the reason
	// (`session`): the page request beside this part re-mints and its
	// Set-Cookie has landed by the time the client reads this answer,
	// so the runtime waits for the page commit and re-requests the part
	// once instead of reloading the document.
	if _, live := ds.verifySessionToken(readSessionCookie(r)); !live {
		ds.writePartReset(w, partResetSession)
		return
	}
	ctx := app.WithRequest(r.Context(), r)
	ctx = store.WithValues(ctx)
	ctx = withRouteMatch(ctx, ds.App.Router, path)
	fill, outcome := ds.App.RenderPartResult(ctx, path, addr)
	if outcome == app.PartReset {
		ds.writePartReset(w, partResetAny)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	// The seed delta carries only values the PAGE answer did not
	// already send: every page seed includes the full route.* family
	// (SeedSplit folds RouteNames()), so pre-marking them sent leaves a
	// delta of just the names this fill's own render seeded.
	sent := map[string]string{}
	for _, k := range store.RouteNames() {
		if v, ok := store.ResolveSeed(ctx, []string{k})[k]; ok {
			if j, err := json.Marshal(v); err == nil {
				sent["p:"+k] = string(j)
			}
		}
	}
	if seed := partialSeedIslandDelta(ctx, string(fill.HTML), sent); seed != "" {
		b.WriteString(seed)
	}
	fmt.Fprintf(&b, `<template data-cui-fill="%s">%s</template>`, stdhtml.EscapeString(addr), fill.HTML)
	fmt.Fprint(w, b.String())
}

// The two reset reasons, and the wire spelling of each. `1` is the
// original opaque reset (any whole-page disagreement the client must
// resolve with a document reload); `session` names the one case the
// client can repair without a reload: the page request beside the part
// re-mints the session, so the part can be re-requested once after the
// page commits.
const (
	partResetAny     = "1"
	partResetSession = "session"
)

// writePartReset is the part 409: no body the client applies, and the
// header that tells it why the part cannot land. reason is one of the
// partReset* constants above.
func (ds *UIHost) writePartReset(w http.ResponseWriter, reason string) {
	w.Header().Set("X-Gofastr-Part-Reset", reason)
	w.WriteHeader(http.StatusConflict)
}

// handlePartialPage returns just the screen content for client-side navigation.
// The runtime.js router swaps the <main> content without a full page reload.
func (ds *UIHost) handlePartialPage(w http.ResponseWriter, r *http.Request, path string) {
	// no-store before ANY branch can return: every partial-shaped
	// response — the prefetch 204 below, a policy DecisionRedirect's
	// 200/303, a DecisionBlock, and the success body — is per-user
	// rendered content, and the re-mint arm additionally carries
	// Set-Cookie (a signed session token) + X-Gofastr-Session, which a
	// shared cache replaying would hand one visitor to another. RFC 7234
	// does not exempt 200+Set-Cookie from caching on its own, and the
	// early exits used to return before the unconditional Set at the
	// bottom, shipping uncacheable-flagged. The success path re-asserts
	// it for its own documentation value.
	w.Header().Set("Cache-Control", "no-store")
	// Stateless-session rollover on the SPA path (#112): a partial
	// navigation must re-mint an invalid/expired token exactly like a
	// full render does; otherwise a restart, key rotation, or expiry
	// leaves this SPA's islands and SSE 401ing until a hard reload.
	// The fresh bare id travels in X-Gofastr-Session so the runtime
	// rewires the SSE meta; the signed token rides Set-Cookie only.
	token := readSessionCookie(r)
	var sessionMinted time.Time // a live session's signing time, on a prefetch
	if _, live := ds.verifySessionToken(token); !live {
		// A prefetch with a dead session gets 204 and no mint: the client
		// never caches a non-partial response, so the eventual real click
		// runs the full rollover below. Serving the prefetch instead would
		// let the click paint entirely from the prefetched entry with the
		// stale token still in place. Islands, SSE, and action scripts
		// then 401 until some later request happens to hit the network.
		if r.Header.Get("X-Gofastr-Prefetch") == "1" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if sess := ds.CreateSession(); sess.Token != "" {
			setSessionCookie(w, r, sess.Token)
			w.Header().Set("X-Gofastr-Session", sess.ID)
		}
	} else if r.Header.Get("X-Gofastr-Prefetch") == "1" {
		// A live session still ends: a prefetched entry kept past that
		// point would paint with no server contact, and its islands,
		// actions and SSE would answer 401. The page's write below says
		// how long the token has left.
		sessionMinted, _ = sessiontoken.MintedAt(token)
	}
	// Declarative redirect on the SPA path: hand the runtime the target
	// via X-Gofastr-Location so it pushState's and loads the partial
	// there (same mechanism as a policy redirect). The client manifest
	// usually rewrites before the request; this covers first load and a
	// stale manifest so a redirect never 404s. The request's query rides
	// along (carryQuery), as on the full-load 308. The target is percent-
	// encoded like http.Redirect's Location: fetch reads header bytes as
	// Latin-1.
	if target, ok := ds.App.ResolveRedirect(path); ok && handler.IsSafeRelativePath(target) {
		w.Header().Set("X-Gofastr-Location", escapeNonASCII(scrubCtl(carryQuery(target, r.URL.RawQuery))))
		w.WriteHeader(http.StatusOK)
		return
	}
	// Mirror handlePage: expose the live *http.Request to ScreenLoader
	// via app.WithRequest so partial-fetched screens can still read URL
	// query (sort, page, filters) just like full-render screens do.
	ctx := app.WithRequest(r.Context(), r)
	ctx = store.WithValues(ctx) // capture producer-seeded values for the partial seed
	ctx, recorded := app.TrackStatus(ctx)
	// Route match for policies/screens on the partial path too, same
	// as handlePage: RouteMatchMiddleware normally installed it.
	ctx = withRouteMatch(ctx, ds.App.Router, path)

	// Intercepting routes (#130 slice 5): the client ASKS for an overlay
	// by sending X-Gofastr-Intercept plus the location it is navigating
	// FROM; the server decides. X-Gofastr-From is attacker-controllable,
	// so it is never trusted as an instruction: InterceptFor re-resolves
	// it against the route table and only agrees when the target screen
	// actually declared that origin. A forged header can therefore change
	// nothing but the wrapper element: policy, params, Load, and content
	// are identical on both paths, so there is nothing to escalate to.
	// No agreement, no X-Gofastr-Overlay, and the client falls back to
	// the ordinary <main> swap.
	var overlay *app.Intercept
	if r.Header.Get("X-Gofastr-Intercept") == "1" {
		overlay, _ = ds.App.Router.InterceptFor(path, fromPath(r))
	}

	var res app.RenderResult
	var err error
	if overlay != nil {
		res, err = ds.App.RenderOverlayResult(ctx, path, fromPath(r), overlay.As)
	} else if from := fromPath(r); from != "" {
		// Subtree partial: the client names the route it is navigating
		// FROM; the server renders only the layout layers the two routes
		// do NOT share and echoes the swap boundary via X-Gofastr-Swap.
		// Same trust posture as the intercept use of this header: it is
		// re-resolved against the route table, and a forged value can
		// only change how much shared chrome gets re-rendered, never
		// policy, params, Load, or content.
		//
		// X-Gofastr-Defer the page request of a
		// navigation whose destination has deferred outlets. The
		// deferred outlets' loaders are skipped and their loading
		// content travels in their place; the fills arrive as separate
		// part requests the client launched beside this one.
		if r.Header.Get("X-Gofastr-Defer") == "1" {
			res, err = ds.App.RenderPartialFromResultDefer(ctx, path, from)
		} else {
			res, err = ds.App.RenderPartialFromResult(ctx, path, from)
		}
	} else {
		res, err = ds.App.RenderPartialResult(ctx, path)
	}
	if err != nil {
		// Whole-page resolver failure on a navigation: the partial arm
		// of the same outcome handlePage answers — the error page (or
		// the not-found page for ErrNotFound) inside the live shell,
		// status carried, never the error text in the body.
		var pe *app.PageError
		if errors.As(err, &pe) {
			ds.serveResolverError(w, r, path, pe)
			return
		}
		ds.serveRenderError(w, r, path, err)
		return
	}
	switch res.Kind {
	case app.DecisionRedirect:
		// MUST be 200 + X-Gofastr-Location, not 3xx. The runtime
		// fetcher uses redirect:'follow' so a 303 here would be chased
		// silently and the header would never reach client JS. The
		// client-side router reads the header and pushState's to the
		// new URL, then loads the partial there.
		//
		// SAFETY: only emit X-Gofastr-Location for safe same-origin
		// relative paths. An absolute, protocol-relative, or scheme-
		// bearing URL would be fed directly into loadPage(), which
		// does a fetch with credentials, turning the partial-
		// redirect signal into a cross-origin XSRF / credential-leak
		// vector. An absolute http(s) URL goes out as
		// X-Gofastr-Document, which the runtime loads as a page and
		// never fetches. For other unsafe URLs fall back to a hard 303
		// redirect; the browser handles those safely (cross-origin
		// redirects don't propagate cookies, javascript:/data: schemes
		// are blocked at the navigation layer).
		if handler.IsSafeRelativePath(res.URL) {
			w.Header().Set("X-Gofastr-Location", escapeNonASCII(res.URL))
			w.WriteHeader(http.StatusOK)
			return
		}
		if writeDocumentRedirect(w, res.URL) {
			return
		}
		http.Redirect(w, r, scrubCtl(res.URL), http.StatusSeeOther)
		return
	case app.DecisionBlock:
		writeBlock(w, r, res)
		return
	}

	// The whole seconds the session has left, counted after the render
	// so a slow loader does not stretch it, let preload.js drop the entry
	// then if it comes before the TTL. Relative, so the browser's clock
	// does not matter.
	if !sessionMinted.IsZero() {
		left := max(time.Until(sessionMinted.Add(sessionMaxAge)), 0)
		w.Header().Set("X-Gofastr-Session-Expires-In", strconv.FormatInt(int64(left/time.Second), 10))
	}
	ds.writePartialResult(w, r, ctx, path, res, overlay, pageStatus(res, recorded()))
}

// writePartialResult writes a rendered partial-navigation result: the
// title/swap headers, the envelope or bare body, and status. The
// success path passes pageStatus (0 is the implicit 200);
// serveNotFoundPartial passes http.StatusNotFound so a missed
// navigation answers partial-shaped with the error status.
// partialHead is the head carrier a partial page body ends with: the
// page-owned metadata, language, direction and skip-link label the
// runtime moves into the kept document. Attribute escaping keeps
// authored head HTML inert until the runtime selects safe meta/link
// elements; a script is never replayed on navigation.
func (ds *UIHost) partialHead(ctx context.Context, path string, comp component.Component) render.HTML {
	lang := ds.App.LangFor(ctx, path)
	if c, ok := comp.(app.ScreenLanger); ok && c.ScreenLang() != "" {
		lang = c.ScreenLang()
	}
	head := map[string]string{"data-cui-page-head": markScreenHead(ds.screenHeadHTML(ctx, path, comp)), "data-cui-lang": lang, "data-cui-dir": i18n.Dir(lang), "data-cui-skip-label": ds.App.SkipLabelFor(ctx, path)}
	// The runtime's own words for the destination's reader: a soft nav
	// keeps the document, so the #gofastr-strings block in <head> would
	// otherwise keep speaking the first page's language. Absent means
	// English, which clears a previous page's table.
	if strs := runtime.StringsJSON(ctx); strs != nil {
		head["data-cui-strings"] = string(strs)
	}
	return render.Tag("template", head)
}

func (ds *UIHost) writePartialResult(w http.ResponseWriter, r *http.Request, ctx context.Context, path string, res app.RenderResult, overlay *app.Intercept, status int) {
	// Deploy skew: a runtime that reads another markup generation must
	// not receive this body (markupskew.go).
	if staleMarkupClient(r) {
		writeStaleMarkupReload(w, r, path, res.SwapLayer)
		return
	}
	// The status arrives decided, before any body byte (DESIGN "Render
	// algorithm"): the success path ranks it through pageStatus, the
	// same ranking the full-page path runs (a partial that silently
	// answered 200 for a 410 page would cache a wrong-status entry),
	// and serveNotFoundPartial's 404 is its own.
	// Screen title: prefer the post-Load effective title from the render
	// (dynamic routes register with an empty/generic title; the loaded
	// instance knows the real one), falling back to the registration-time
	// title. Percent-encode it: a title with non-ASCII (e.g. the em-dash
	// in "Docs — GoFastr") sent raw in an HTTP header is non-conformant
	// and a reader decodes the UTF-8 bytes as Latin-1 ("Docs â GoFastr").
	// The runtime must decodeURIComponent it.
	title := res.Title
	if title == "" && res.Kind != app.DecisionRenderAlt {
		if scr, _, ok := ds.App.Router.Resolve(path); ok {
			title = scr.Title
		}
	}
	// Same invisible-character posture as the full page's <title> element:
	// the runtime writes the decoded header straight into document.title,
	// so the header is the tab title. Strip before the suffix is appended.
	title = textsafe.StripInvisible(title)
	if title == "" {
		// An untitled screen's full document is titled with the app name
		// (core-ui/app renderPage); the partial says the same, or the
		// runtime keeps the previous page's title and announcement.
		title = textsafe.StripInvisible(ds.App.Name)
	} else if suffix := " — " + ds.App.Name; !strings.HasSuffix(title, suffix) {
		title += suffix
	}
	if title != "" {
		w.Header().Set("X-Gofastr-Title", url.PathEscape(title))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Gofastr-Partial", "true")
	if res.SwapLayer != "" {
		// Names the layout layer the body renders BELOW; the client swaps
		// the matching data-cui-layout-slot cell. Absent when the body is
		// bare screen content (whole-main swap / full-fetch fallback).
		//
		// Version skew (DESIGN "Mixed versions"): a fills-less client
		// (no X-Gofastr-Fills: 2 — the pre-layout runtime, or a page
		// whose document holds no outlet/area marker) cannot receive the
		// kept layers' fills: the bare body answers only the primary,
		// and the old runtime would keep stale outlets with no repair.
		// Answer "!reload" instead — a swap key no DOM holds, so every
		// runtime's missing-slot repair full-loads the destination and
		// the real page (fills inline, an outlet-404's status included)
		// arrives whole.
		swap := res.SwapLayer
		if r.Header.Get("X-Gofastr-Fills") != "2" && len(res.Fills) > 0 {
			swap = "!reload"
		}
		w.Header().Set("X-Gofastr-Swap", escapeLayerKey(swap))
	}
	// The page answer's keyed-transition pick
	// the runtime adds it to the view-transition types beside the
	// direction, ignoring names the document's data-cui-vt-kinds does
	// not declare. Absent when nothing was picked.
	if res.Transition != "" {
		w.Header().Set("X-Gofastr-Transition", res.Transition)
	}
	if overlay != nil {
		// The client mounts overlay chrome only when the SERVER says so.
		w.Header().Set("X-Gofastr-Overlay", overlay.As.String())
	}
	// Re-assert no-store (handlePartialPage sets it before any branch can
	// return; see that comment for the threat model — every partial-shaped
	// body is per-user rendered content, the not-found partial included).
	w.Header().Set("Cache-Control", "no-store")
	res.HTML = render.Join(res.HTML, ds.partialHead(withRenderAlt(ctx, res), path, res.Component))
	// Fills envelope a fills-capable client that
	// kept layout layers with tree-layout outlets gets the partial as an
	// envelope — the seed island (scanned over the primary payload AND
	// every fill), then one <template data-cui-fill> per fill, the
	// primary first (addressed by the bare swap key) and the kept
	// layers' non-primary fills after. Anything else keeps today's body
	// shape. Under X-Gofastr-Defer a deferred
	// outlet's fill here is its LOADING content; the real fill arrives
	// as the matching part request.
	if r.Header.Get("X-Gofastr-Fills") == "2" && len(res.Fills) > 0 {
		w.Header().Set("X-Gofastr-Envelope", "2")
		scanned := string(res.HTML)
		for _, f := range res.Fills {
			scanned += string(f.HTML)
		}
		var b strings.Builder
		if seed := partialSeedIsland(ctx, scanned); seed != "" {
			b.WriteString(seed)
		}
		// HTML attribute escaping, not Go quoting (%q): a {param} group's
		// layer key carries the resolved route value, and %q's `\"` ends an
		// HTML attribute.
		fmt.Fprintf(&b, `<template data-cui-fill="%s">%s</template>`, stdhtml.EscapeString(res.SwapLayer), res.HTML)
		for _, f := range res.Fills {
			fmt.Fprintf(&b, `<template data-cui-fill="%s">%s</template>`, stdhtml.EscapeString(f.Addr), f.HTML)
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		fmt.Fprint(w, b.String())
		return
	}
	if status != 0 {
		w.WriteHeader(status)
	}
	fmt.Fprint(w, partialSeedIsland(ctx, string(res.HTML))+string(res.HTML))
}
