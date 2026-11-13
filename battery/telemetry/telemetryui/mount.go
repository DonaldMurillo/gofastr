package telemetryui

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// Config places the screens in an app that has no admin to hold them.
type Config struct {
	// Path is where the screens are served. Default "/telemetry".
	Path string
	// Authorize admits a caller to the screens. Required: the pages
	// show request paths, client addresses and whatever the app logs,
	// so there is no open default. It runs on every request.
	Authorize func(ctx context.Context) bool
}

// Plugin mounts the screens on the app's UI host. It implements
// framework.Plugin.
type Plugin struct {
	tel *telemetry.Plugin
	cfg Config
}

// New returns the plugin that serves tel's screens. Register it after
// the UI host is mounted.
func New(tel *telemetry.Plugin, cfg Config) *Plugin { return &Plugin{tel: tel, cfg: cfg} }

// Name implements framework.Plugin.
func (*Plugin) Name() string { return "telemetry-ui" }

// Init registers the two screens behind Authorize.
func (p *Plugin) Init(app *framework.App) error {
	if p.tel == nil {
		return errors.New("telemetryui: New needs the telemetry plugin")
	}
	if p.cfg.Authorize == nil {
		return errors.New("telemetryui: Config.Authorize is required: these pages show request paths, client addresses and log lines")
	}
	path := strings.TrimRight(p.cfg.Path, "/")
	if path == "" {
		path = "/telemetry"
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New("telemetryui: Config.Path must start with /")
	}
	var host *uihost.UIHost
	for _, m := range app.Mountables() {
		if h, ok := m.(*uihost.UIHost); ok {
			host = h
			break
		}
	}
	if host == nil {
		return errors.New("telemetryui: the screens render through the app's UI host; mount one first (framework.NewUIHostApp or uihost.New)")
	}

	screens := NewScreens(p.tel, path, true)
	screens.Live = func(list string) string { return path + "/_rows/" + list }
	allowed := func(ctx context.Context) bool { return p.cfg.Authorize(ctx) }
	policy := appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		if allowed(ctx) {
			return decide.Allow()
		}
		return decide.Block(http.StatusForbidden)
	})
	layout := appui.NewLayout("gofastr-telemetry", appui.LayoutSpec{}, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Stack(ui.StackConfig{Screen: true, Gap: ui.GapNone},
			ui.Container(ui.ContainerConfig{Width: ui.ContainerWide, Pad: ui.ContainerPadPage}, l.Primary()))
	})
	group := appui.NewScreenGroup(path, layout, policy).Standalone()
	add := func(at string, title i18n.Phrase, draw func(context.Context) render.HTML) {
		group.Screen(appui.NewScreen(at, &screen{allowed: allowed, title: title, draw: draw}).
			WithTitle(title.Text(context.Background())), nil)
	}
	add(screens.RequestsPath(), i18nui.TelemetryRequests, screens.Requests)
	add(screens.LogsPath(), i18nui.TelemetryLogs, screens.Logs)
	host.App.Router.ScreenGroup(group)

	// A list's rows alone, for the newest page's timer. The same gate
	// as the pages, asked on every fetch: a caller whose access ended
	// stops getting rows at the next tick.
	rows := func(draw func(context.Context) render.HTML) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := appui.WithRequest(r.Context(), r)
			w.Header().Set("Cache-Control", "private, no-store")
			if !allowed(ctx) {
				i18n.HTTPError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(draw(ctx)))
		})
	}
	app.Router().Get(path+"/_rows/requests", rows(screens.RequestsRows))
	app.Router().Get(path+"/_rows/logs", rows(screens.LogsRows))

	// The bare path goes to the first screen. The target is fixed, and
	// the screen it names is the one that asks who is calling.
	target := screens.RequestsPath()
	app.Router().Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusSeeOther)
	}))
	return nil
}

// screen is one page as a screen component.
type screen struct {
	component.ContextOnly
	allowed func(context.Context) bool
	title   i18n.Phrase
	draw    func(context.Context) render.HTML
}

// ScreenTitle names the page in the caller's language.
func (s *screen) ScreenTitle(ctx context.Context) string { return s.title.Text(ctx) }

// RenderCtx draws the page. The group's policy has already refused
// everyone else; asking again keeps the page's data from depending on
// that alone.
func (s *screen) RenderCtx(ctx context.Context) render.HTML {
	if !s.allowed(ctx) {
		return ""
	}
	return s.draw(ctx)
}
