package admin

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
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

	// Title is the page title, its crumb and its nav and palette label.
	Title string

	// Nav places the page in the sidebar (Group, Order, Icon). Nil, or
	// Hide, leaves it out of the sidebar; the palette still lists it.
	Nav *entity.EntityNav

	// Access narrows who sees the page beyond the admin gate. Nil admits
	// every admin. A refused caller gets 403 and no nav entry.
	Access func(ctx context.Context) bool

	// Build draws the page body. It runs with the caller's own context:
	// the admin's elevation never reaches app code. A panic or an error
	// fails the page with a generic message and a log line.
	Build func(r *http.Request) (component.Component, error)
}

// Card is one of the app's own dashboard cards.
type Card struct {
	// Key names the card: a lowercase slug, unique among Cards.
	Key string

	// Title is the card's heading.
	Title string

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
	Label string
	// Href is a same-origin path.
	Href string
	// Icon is a registered kit icon name, or "".
	Icon string
}

// reservedPagePath reports a path the admin draws itself, or the prefix
// of one of its routes, which an app page may not take.
func reservedPagePath(p string) bool {
	for _, own := range []string{"/search", "/queue", "/audit", "/rbac", "/modules", "/entities", "/api"} {
		if p == own || strings.HasPrefix(p, own+"/") {
			return true
		}
	}
	// "/_count", "/_palette", "/_card" and any later route of the admin's
	// own start with an underscore.
	return p == "/" || strings.HasPrefix(p, "/_")
}

// mountPages registers each app page as a screen. Access is a screen
// policy, so a refused caller is answered before Build runs.
func (b *Battery) mountPages(group *appui.ScreenGroup) {
	for _, p := range b.cfg.Pages {
		s := b.screen(group, p.Path, "", false, func(ctx context.Context, _ map[string]string) render.HTML {
			return b.buildSlot(ctx, "page "+p.Path, p.Build)
		})
		as := s.Component.(*adminScreen)
		as.title = func(context.Context, map[string]string) string { return p.Title }
		s.Title = p.Title
		if p.Access != nil {
			s.WithPolicy(appui.PolicyFunc(func(ctx context.Context) appui.Decision {
				if p.Access(ctx) {
					return decide.Allow()
				}
				return decide.Block(http.StatusForbidden, http.StatusText(http.StatusForbidden))
			}))
		}
	}
}

// buildSlot runs an app Build with the caller's context and draws what
// it returns. A panic, an error or a nil component draws a generic
// message; the log names the slot, never what the page read.
func (b *Battery) buildSlot(ctx context.Context, slot string, build func(*http.Request) (component.Component, error)) (out render.HTML) {
	failed := func(reason string) render.HTML {
		b.logger().Error("admin: app slot failed", "slot", slot, "reason", reason)
		return ui.Callout(ui.CalloutConfig{Variant: ui.StatusDanger}, render.Text(i18nui.T(ctx, i18nui.KeyAdminFailed)))
	}
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
