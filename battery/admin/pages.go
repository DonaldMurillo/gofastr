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

// Page is one of the app's own admin pages, drawn in the shell behind
// the gate, with breadcrumbs and a palette entry.
type Page struct {
	// Path is the page's path under the admin, e.g. "/reports". It may
	// not take one of the admin's own paths.
	Path string

	// Title is the page title, its crumb and its nav and palette label,
	// in the request's language.
	Title i18n.Phrase

	// TabTitle, when set, names what the page is showing right now for
	// the browser tab, history and a screen reader's arrival
	// announcement: a page whose query picks between views ("Failed
	// requests", "Request 7e22a54e") says which. Title stays the nav,
	// crumb and palette label. It runs for callers the page admits,
	// with their own context; an empty answer falls back to Title.
	TabTitle func(ctx context.Context) string

	// Nav places the page in the sidebar (Group, Order, Icon). Nil, or
	// Hide, leaves it out of the sidebar; the palette still lists it.
	Nav *entity.EntityNav

	// Access narrows who sees the page beyond the admin gate. Nil admits
	// every admin. A refused caller gets 403 and no nav entry. Like Build,
	// it runs with the caller's own context, never the admin's elevation.
	Access func(ctx context.Context) bool

	// Build draws the page body. It runs with the caller's own context:
	// the admin's elevation never reaches app code. A panic or an error
	// fails the page with a generic message and a log line.
	Build func(r *http.Request) (component.Component, error)

	// Part, when set, draws the piece of the page that keeps itself
	// current: a list's rows. The admin serves it alone at
	// PagePartPath(prefix, Path), behind the gate and this page's
	// Access, with the caller's own context, as Build runs. The page
	// points a polled region at it:
	//
	//	html.Div(html.DivConfig{ExtraAttrs: html.Attrs{
	//	    "data-cui-poll":     "10s",
	//	    "data-cui-poll-src": admin.PagePartPath("/admin", "/reports"),
	//	}}, rows)
	//
	// The request's query string reaches Part as it reaches Build.
	Part func(r *http.Request) (component.Component, error)
}

// PagePartPath is where the admin at prefix serves the Part of the page
// at pagePath.
func PagePartPath(prefix, pagePath string) string {
	return strings.TrimRight(prefix, "/") + "/_part" + pagePath
}

// Card is one of the app's own dashboard cards.
type Card struct {
	// Key names the card: a lowercase slug, unique among Cards.
	Key string

	// Title is the card's heading, in the request's language.
	Title i18n.Phrase

	// Build draws the card body with the caller's own context. A panic or
	// an error draws a generic message in the card and logs.
	Build func(r *http.Request) (component.Component, error)

	// Poll, when positive, redraws the card on that interval (at least
	// five seconds; the runtime clamps shorter ones). Zero draws it once.
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

// reservedPagePath reports a path the admin draws itself, or the prefix
// of one of its routes, which an app page may not take.
func reservedPagePath(p string) bool {
	for _, own := range []string{"/search", "/shortcuts", "/account", "/queue", "/audit", "/rbac", "/modules", "/emails", "/entities", "/api"} {
		if p == own || strings.HasPrefix(p, own+"/") {
			return true
		}
	}
	// "/_count", "/_palette", "/_card" and any later route of the admin's
	// own start with an underscore.
	return p == "/" || strings.HasPrefix(p, "/_")
}

// tabTitle is the page's title for the browser tab: TabTitle's answer,
// asked with the caller's own context (an app page's screen is never
// elevated) and only after the page's Access policy admitted the
// caller, which refuses before a screen loads; and Title for an empty answer and for a TabTitle
// that panics. Nil when the page sets none.
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
		// Title here, not an empty answer: the screen's own fallback
		// is the title in the default language.
		if t := p.TabTitle(ctx); t != "" {
			return t
		}
		return title
	}
}

// mountPages registers each app page as a screen. Access is a screen
// policy, so a refused caller is answered before Build runs.
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

// pageAllows reports whether p's Access admits the caller, asked without
// the admin's elevation: the sidebar and the palette ask it while drawing
// elevated screens. An Access that panics refuses, so one page's policy
// hides that page and leaves the rest of the admin drawing.
func (b *Battery) pageAllows(ctx context.Context, p Page) bool {
	if p.Access == nil {
		return true
	}
	return b.askPolicy("Page "+p.Path+" Access", func() bool { return p.Access(crud.WithoutElevation(ctx)) })
}

// buildSlot runs an app Build with the caller's context and draws what
// it returns. The admin's elevation is stripped first: a dashboard card
// draws inside the elevated dashboard. A panic, an error or a nil component draws a generic
// message; the log names the slot, never what the page read.
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

// cardPath is where a polled app card redraws.
func (b *Battery) cardPath(key string) string {
	return b.cfg.PathPrefix + "/_card/" + key
}

// mountParts mounts GET <prefix>/_part/<page path> for every page with a
// Part: that part alone, drawn with the caller's own context. The route
// sits in the gated group, and the page's own Access is asked here: a
// screen policy answers for the page, not for this route.
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

// mountCards mounts GET <prefix>/_card/<key> for every polled card: the
// card body, drawn with the caller's own context.
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
