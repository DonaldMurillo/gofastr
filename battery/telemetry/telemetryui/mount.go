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
	// Path is where the screens are served.
	Path string
	// Authorize admits a caller to the screens.
	Authorize func(ctx context.Context) bool
}

// Plugin mounts the screens on the app's UI host.
type Plugin struct {
	tel *telemetry.Plugin
	cfg Config
}

// New returns the plugin that serves tel's screens.
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

	screens := NewScreens(p.tel, path, false)
	screens.Live = func(list string) string { return path + "/_rows/" + list }
	allowed := func(ctx context.Context) bool { return p.cfg.Authorize(ctx) }
	policy := appui.PolicyFunc(func(ctx context.Context) appui.Decision {
		if allowed(ctx) {
			return decide.Allow()
		}
		return decide.Block(http.StatusForbidden)
	})
	spec := appui.LayoutSpec{Areas: []appui.AreaSpec{{Name: "tabs"}}}
	layout := appui.NewLayout("gofastr-telemetry", spec, func(_ context.Context, l *appui.LayoutTree) render.HTML {
		return ui.Container(ui.ContainerConfig{Width: ui.ContainerWide, Pad: ui.ContainerPadPage},
			l.RouteArea("tabs", func(ctx context.Context, m appui.Match) render.HTML {
				return screens.Tabs(ctx, m.Path())
			}),
			l.Primary())
	})
	group := appui.NewScreenGroup(path, layout, policy).Standalone()
	add := func(at string, title i18n.Phrase, name func(context.Context) string, draw func(context.Context) render.HTML) {
		if name == nil {
			name = func(ctx context.Context) string { return title.Text(ctx) }
		}
		group.Screen(appui.NewScreen(at, &screen{allowed: allowed, name: name, draw: draw}).
			WithTitle(title.Text(context.Background())), nil)
	}
	add(screens.OverviewPath(), i18nui.TelemetryOverview, nil, screens.Overview)
	add(screens.RequestsPath(), i18nui.TelemetryRequests, screens.RequestsTitle, screens.Requests)
	add(screens.IssuesPath(), i18nui.TelemetryIssues, nil, screens.Issues)
	add(screens.LogsPath(), i18nui.TelemetryLogs, screens.LogsTitle, screens.Logs)
	host.App.Router.ScreenGroup(group)

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
	app.Router().Get(path+"/_rows/overview", rows(screens.OverviewRows))
	app.Router().Get(path+"/_rows/requests", rows(screens.RequestsRows))
	app.Router().Get(path+"/_rows/logs", rows(screens.LogsRows))

	target := screens.OverviewPath()
	app.Router().Get(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		to := target
		if r.URL.RawQuery != "" {
			to += "?" + r.URL.Query().Encode()
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
	}))
	return nil
}

type screen struct {
	component.ContextOnly
	allowed func(context.Context) bool
	name    func(context.Context) string
	draw    func(context.Context) render.HTML
}

// ScreenTitle names the page in the caller's language.
func (s *screen) ScreenTitle(ctx context.Context) string { return s.name(ctx) }

// RenderCtx draws the page.
func (s *screen) RenderCtx(ctx context.Context) render.HTML {
	if !s.allowed(ctx) {
		return ""
	}
	return s.draw(ctx)
}
