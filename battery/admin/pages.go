package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Page is one of the app's own admin pages, drawn in the shell behind the
// gate.
type Page struct {
	// Path is the page's path under the admin, e.g. "/reports"; it may not
	// take one of the admin's own paths.
	Path string

	// Title is the page title, its crumb and its nav and palette label.
	Title i18n.Phrase

	// TabTitle, when set, names the current view for the browser tab; an empty
	// answer falls back to Title.
	TabTitle func(ctx context.Context) string

	// Nav places the page in the sidebar. Nil, or Hide, leaves it out of the
	// sidebar but not the palette.
	Nav *entity.EntityNav

	// Access narrows who sees the page beyond the admin gate; a refused caller
	// gets 403. Nil admits every admin.
	Access func(ctx context.Context) bool

	// Build draws the page body with the caller's own context, never the
	// admin's elevation.
	Build func(r *http.Request) (component.Component, error)

	// Part, when set, draws the piece of the page a polled region refreshes,
	// served alone at PagePartPath.
	Part func(r *http.Request) (component.Component, error)
}

// PagePartPath is where the admin at prefix serves the Part of the page at
// pagePath.
func PagePartPath(prefix, pagePath string) string {
	return strings.TrimRight(prefix, "/") + "/_part" + pagePath
}

// Card is one of the app's own dashboard cards.
type Card struct {
	// Key names the card: a lowercase slug, unique among Cards.
	Key string

	// Title is the card's heading, in the request's language.
	Title i18n.Phrase

	// Build draws the card body with the caller's own context.
	Build func(r *http.Request) (component.Component, error)

	// Poll, when positive, redraws the card on that interval. Zero draws it
	// once.
	Poll time.Duration
}

// Link is an extra sidebar link.
type Link struct {
	// Group is the nav group key; "" is the default group.
	Group string
	// Label is the link's text, in the request's language.
	Label i18n.Phrase
	// Href is a same-origin path.
	Href string
	// Icon is a registered kit icon name, or "".
	Icon string
}

func reservedPagePath(p string) bool {
	for _, own := range []string{"/search", "/shortcuts", "/account", "/queue", "/audit", "/rbac", "/modules", "/emails", "/entities", "/api"} {
		if p == own || strings.HasPrefix(p, own+"/") {
			return true
		}
	}
	return p == "/" || strings.HasPrefix(p, "/_")
}

func (b *Battery) tabTitle(p Page) func(ctx context.Context, _ map[string]string) string {
	if p.TabTitle == nil {
		return nil
	}
	return func(ctx context.Context, _ map[string]string) (title string) {
		title = p.Title.Text(ctx)
		defer func() {
			if r := recover(); r != nil {
				slog.ErrorContext(ctx, "admin: page TabTitle panicked", "page", p.Path, "panic", textsafe.Recovered(r))
				title = p.Title.Text(ctx)
			}
		}()
		if t := p.TabTitle(ctx); t != "" {
			return t
		}
		return title
	}
}

func (b *Battery) mountPages(group *appui.ScreenGroup) {
	for _, p := range b.cfg.Pages {
		s := b.screenNamed(group, p.Path, p.Title, b.tabTitle(p), false, func(ctx context.Context, _ map[string]string) render.HTML {
			return b.buildSlot(ctx, "page "+p.Path, p.Build)
		})
		if p.Access != nil {
			s.WithPolicy(appui.PolicyFunc(func(ctx context.Context) appui.Decision {
				if b.pageAllows(ctx, p) {
					return decide.Allow()
				}
				return decide.Block(http.StatusForbidden)
			}))
		}
	}
}

func (b *Battery) pageAllows(ctx context.Context, p Page) bool {
	if p.Access == nil {
		return true
	}
	return b.askPolicy("Page "+p.Path+" Access", func() bool { return p.Access(crud.WithoutElevation(ctx)) })
}

func (b *Battery) buildSlot(ctx context.Context, slot string, build func(*http.Request) (component.Component, error)) (out render.HTML) {
	failed := func(reason string) render.HTML {
		b.logger().Error("admin: app slot failed", "slot", slot, "reason", reason)
		return ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.AdminFailed.Text(ctx)))
	}
	ctx = crud.WithoutElevation(ctx)
	r := appui.RequestFromContext(ctx)
	if r == nil {
		return failed("no request")
	}
	defer func() {
		if rec := recover(); rec != nil {
			out = failed("panic")
		}
	}()
	c, err := build(r.WithContext(ctx))
	if err != nil {
		return failed(fmt.Sprintf("build: %T", err))
	}
	if c == nil {
		return failed("nil component")
	}
	html, err := component.SafeRenderCtx(ctx, c)
	if err != nil {
		return failed("render")
	}
	return html
}

func (b *Battery) cardPath(key string) string {
	return b.cfg.PathPrefix + "/_card/" + key
}

func (b *Battery) mountParts(r *router.Router) {
	for _, p := range b.cfg.Pages {
		if p.Part == nil {
			continue
		}
		r.Get(PagePartPath(b.cfg.PathPrefix, p.Path), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := appui.WithRequest(r.Context(), r)
			w.Header().Set("Cache-Control", "private, no-store")
			if !b.pageAllows(ctx, p) {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(b.buildSlot(ctx, "page part "+p.Path, p.Part)))
		}))
	}
}

func (b *Battery) mountCards(r *router.Router) {
	r.Get(b.cfg.PathPrefix+"/_card/{key}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, c := range b.cfg.Cards {
			if c.Key == r.PathValue("key") && c.Poll > 0 {
				ctx := appui.WithRequest(r.Context(), r)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(b.buildSlot(ctx, "card "+c.Key, c.Build)))
				return
			}
		}
		http.NotFound(w, r)
	}))
}
