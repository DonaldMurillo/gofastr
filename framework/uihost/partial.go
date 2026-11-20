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

func (ds *UIHost) handlePartRequest(w http.ResponseWriter, r *http.Request, path, addr string) {
	w.Header().Set("Cache-Control", "no-store")
	if staleMarkupClient(r) {
		ds.writePartReset(w, partResetAny)
		return
	}
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

const (
	partResetAny     = "1"
	partResetSession = "session"
)

func (ds *UIHost) writePartReset(w http.ResponseWriter, reason string) {
	w.Header().Set("X-Gofastr-Part-Reset", reason)
	w.WriteHeader(http.StatusConflict)
}

// partialSession is a client navigation's session step: a prefetch with
// a dead session answers 204 (done), any other request with one gets a
// fresh session, and a prefetch with a live one reports when it was
// minted.
func (ds *UIHost) partialSession(w http.ResponseWriter, r *http.Request) (minted time.Time, done bool) {
	w.Header().Set("Cache-Control", "no-store")
	token := readSessionCookie(r)
	if _, live := ds.verifySessionToken(token); !live {
		if r.Header.Get("X-Gofastr-Prefetch") == "1" {
			w.WriteHeader(http.StatusNoContent)
			return time.Time{}, true
		}
		if sess := ds.CreateSession(); sess.Token != "" {
			setSessionCookie(w, r, sess.Token)
			w.Header().Set("X-Gofastr-Session", sess.ID)
		}
	} else if r.Header.Get("X-Gofastr-Prefetch") == "1" {
		minted, _ = sessiontoken.MintedAt(token)
	}
	return minted, false
}

func (ds *UIHost) handlePartialPage(w http.ResponseWriter, r *http.Request, path string) {
	sessionMinted, done := ds.partialSession(w, r)
	if done {
		return
	}
	if target, ok := ds.App.ResolveRedirect(path); ok && handler.IsSafeRelativePath(target) {
		w.Header().Set("X-Gofastr-Location", escapeNonASCII(scrubCtl(carryQuery(target, r.URL.RawQuery))))
		w.WriteHeader(http.StatusOK)
		return
	}
	ctx := app.WithRequest(r.Context(), r)
	ctx = store.WithValues(ctx)
	ctx, recorded := app.TrackStatus(ctx)
	ctx = withRouteMatch(ctx, ds.App.Router, path)

	var overlay *app.Intercept
	if r.Header.Get("X-Gofastr-Intercept") == "1" {
		overlay, _ = ds.App.Router.InterceptFor(path, fromPath(r))
	}

	var res app.RenderResult
	var err error
	if overlay != nil {
		res, err = ds.App.RenderOverlayResult(ctx, path, fromPath(r), overlay.As)
	} else if from := fromPath(r); from != "" {
		if r.Header.Get("X-Gofastr-Defer") == "1" {
			res, err = ds.App.RenderPartialFromResultDefer(ctx, path, from)
		} else {
			res, err = ds.App.RenderPartialFromResult(ctx, path, from)
		}
	} else {
		res, err = ds.App.RenderPartialResult(ctx, path)
	}
	if err != nil {
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

	if !sessionMinted.IsZero() {
		left := max(time.Until(sessionMinted.Add(sessionMaxAge)), 0)
		w.Header().Set("X-Gofastr-Session-Expires-In", strconv.FormatInt(int64(left/time.Second), 10))
	}
	ds.writePartialResult(w, r, withOutcome(ctx, res), path, res, overlay, pageStatus(res, recorded()))
}

func (ds *UIHost) partialHead(ctx context.Context, path string, comp component.Component) render.HTML {
	lang := ds.App.LangFor(ctx, path)
	if c, ok := comp.(app.ScreenLanger); ok && c.ScreenLang() != "" {
		lang = c.ScreenLang()
	}
	head := map[string]string{"data-cui-page-head": markScreenHead(ds.screenHeadHTML(ctx, path, comp)), "data-cui-lang": lang, "data-cui-dir": i18n.Dir(lang), "data-cui-skip-label": ds.App.SkipLabelFor(ctx, path)}
	if strs := runtime.StringsJSON(ctx); strs != nil {
		head["data-cui-strings"] = string(strs)
	}
	return render.Tag("template", head)
}

func (ds *UIHost) writePartialResult(w http.ResponseWriter, r *http.Request, ctx context.Context, path string, res app.RenderResult, overlay *app.Intercept, status int) {
	if staleMarkupClient(r) {
		writeStaleMarkupReload(w, r, path, res.SwapLayer)
		return
	}
	title := res.Title
	if title == "" && res.Kind != app.DecisionRenderAlt {
		if scr, _, ok := ds.App.Router.Resolve(path); ok {
			title = scr.Title
		}
	}
	title = textsafe.StripInvisible(title)
	if title == "" {
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
		swap := res.SwapLayer
		if r.Header.Get("X-Gofastr-Fills") != "2" && len(res.Fills) > 0 {
			swap = "!reload"
		}
		w.Header().Set("X-Gofastr-Swap", escapeLayerKey(swap))
	}
	if res.Transition != "" {
		w.Header().Set("X-Gofastr-Transition", res.Transition)
	}
	if overlay != nil {
		w.Header().Set("X-Gofastr-Overlay", overlay.As.String())
	}
	w.Header().Set("Cache-Control", "no-store")
	res.HTML = render.Join(res.HTML, ds.partialHead(withRenderAlt(ctx, res), path, res.Component))
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
