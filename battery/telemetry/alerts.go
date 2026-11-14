package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// AlertKind is what an alert rule counts.
type AlertKind string

const (
	// AlertFailed counts requests that failed: a 5xx, a panic, a job
	// that returned an error.
	AlertFailed AlertKind = "failed"
	// AlertSlow counts requests at or over Config.SlowThreshold.
	AlertSlow AlertKind = "slow"
	// AlertDisk watches how full the disk holding the telemetry
	// directory is. Its rule's AtLeast is a percentage from 1 to 100,
	// and it takes no Within and no Route. It resolves five points
	// below its line.
	AlertDisk AlertKind = "disk"
)

// AlertRule fires when at least AtLeast requests of its Kind finished
// within the last Within. It resolves when the count has fallen to
// half of AtLeast or less.
type AlertRule struct {
	// Name identifies the rule in a notification. Required, unique.
	Name string
	Kind AlertKind
	// AtLeast is the count that fires the rule. At least 1.
	AtLeast int
	// Within is the window counted, from one minute to one hour.
	// Zero means five minutes.
	Within time.Duration
	// Route, when set, counts only that route ("POST /checkout").
	Route string
}

// Alert is a rule starting to fire, or resolving.
type Alert struct {
	Rule AlertRule
	// Firing is true when the rule started firing and false when it
	// resolved.
	Firing bool
	// Count is the requests counted in the window at that moment.
	Count int
	// At is when the change was noticed; Since is when the rule started
	// firing.
	At, Since time.Time
}

// Text says the alert in one line, for a chat message or a subject.
func (a Alert) Text() string {
	if a.Rule.Kind == AlertDisk {
		if !a.Firing {
			return fmt.Sprintf("Resolved: %s. The disk is %d%% full, down from at least %d%%.", a.Rule.Name, a.Count, a.Rule.AtLeast)
		}
		return fmt.Sprintf("Alert: %s. The disk is %d%% full (the rule fires at %d%%).", a.Rule.Name, a.Count, a.Rule.AtLeast)
	}
	what := "failed requests"
	if a.Rule.Kind == AlertSlow {
		what = "slow requests"
	}
	scope := ""
	if a.Rule.Route != "" {
		scope = " on " + a.Rule.Route
	}
	within := a.Rule.Within
	if within == 0 {
		within = 5 * time.Minute
	}
	if !a.Firing {
		return fmt.Sprintf("Resolved: %s. %d %s%s in the last %s, down from at least %d.", a.Rule.Name, a.Count, what, scope, within, a.Rule.AtLeast)
	}
	return fmt.Sprintf("Alert: %s. %d %s%s in the last %s (the rule fires at %d).", a.Rule.Name, a.Count, what, scope, within, a.Rule.AtLeast)
}

// defaultAlerts is what an app gets with no rules of its own: told when
// requests are failing in numbers and when the disk is nearly full, and
// nothing else.
var defaultAlerts = []AlertRule{
	{Name: "failed requests", Kind: AlertFailed, AtLeast: 10, Within: 5 * time.Minute},
	// The files this plugin writes are capped, but nothing else on the
	// machine is: a database, an upload directory, the system's logs.
	{Name: "disk nearly full", Kind: AlertDisk, AtLeast: 90},
}

// alertEvery is how often the rules are checked.
const alertEvery = 15 * time.Second

func fromEvent(e itel.AlertEvent) Alert {
	return Alert{
		Rule:   AlertRule{Name: e.Rule.Name, Kind: AlertKind(e.Rule.Kind), AtLeast: e.Rule.AtLeast, Within: e.Rule.Within, Route: e.Rule.Route},
		Firing: e.Firing, Count: e.Count, At: e.At, Since: e.Since,
	}
}

// newAlerts builds the counter for the configured rules, or the default
// one, or nil when alerts are off.
func (p *Plugin) newAlerts() (*itel.Alerts, error) {
	if p.cfg.NoAlerts {
		return nil, nil
	}
	rules := p.cfg.Alerts
	if len(rules) == 0 {
		rules = defaultAlerts
	}
	internal := make([]itel.AlertRule, len(rules))
	for i, r := range rules {
		internal[i] = itel.AlertRule{Name: r.Name, Kind: itel.AlertKind(r.Kind), AtLeast: r.AtLeast, Within: r.Within, Route: r.Route}
	}
	return itel.NewAlerts(internal)
}

// watch runs every alertEvery until stop is closed: it writes the
// request counts of the minutes that have ended, and checks the alert
// rules when there are any. Each change of a rule is a log line, and
// goes to Config.Notify when there is one.
func (p *Plugin) watch(app *framework.App, w *itel.Writer, counts *itel.Rollup, alerts *itel.Alerts, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	tick := time.NewTicker(alertEvery)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-tick.C:
			p.tick(app, w, counts, alerts, now)
		}
	}
}

// tick is one round of watch.
func (p *Plugin) tick(app *framework.App, w *itel.Writer, counts *itel.Rollup, alerts *itel.Alerts, now time.Time) {
	// A failed write is counted by the writer, and the minutes it held
	// are gone: counts are not queued.
	_ = w.FlushStats(counts, false)
	// Once a minute, in the round that first sees the minute: a
	// reading of the machine, and the heartbeat.
	minute := now.Unix() / 60
	fresh := minute != p.hostMinute
	if fresh {
		p.hostMinute = minute
		if r, ok := p.readMachine(app, now); ok {
			_ = w.WriteHost(r)
			p.latestHost.Store(&r)
			if alerts != nil {
				alerts.SetDisk(r.DiskPercent())
			}
		}
	}
	if alerts != nil {
		p.checkAlerts(app, alerts, now)
	}
	// Last: a monitor that is slow to answer holds up nothing else.
	if fresh {
		p.beat(app)
	}
}

// readMachine takes one reading, with a net under it: this runs on the
// watch goroutine, where a panic would end the app over a number for a
// chart.
func (p *Plugin) readMachine(app *framework.App, now time.Time) (r itel.HostReading, ok bool) {
	read := p.readHost
	if read == nil {
		return r, false
	}
	defer func() {
		if v := recover(); v != nil {
			// The results are still their zero values: no reading.
			app.Logger().Error("telemetry: reading the machine panicked", "panic", textsafe.Recovered(v))
		}
	}()
	return read(now), true
}

func (p *Plugin) checkAlerts(app *framework.App, alerts *itel.Alerts, now time.Time) {
	for _, e := range alerts.Check(now) {
		a := fromEvent(e)
		logger := app.Logger()
		if a.Firing {
			logger.Error("telemetry.alert", "rule", a.Rule.Name, "state", "firing", "count", a.Count, "text", a.Text())
		} else {
			logger.Info("telemetry.alert", "rule", a.Rule.Name, "state", "resolved", "count", a.Count, "text", a.Text())
		}
		if p.cfg.Notify != nil {
			p.notify(app, a)
		}
	}
}

// notify calls the app's notifier with a deadline, on a goroutine of
// its own. One that returns in time is waited for. One that hangs is
// left behind at the deadline, or when the app stops: the watcher goes
// on to the next check, and stopping does not wait for it. A notifier
// that ignores its context keeps its goroutine; that is the notifier's
// to fix, and the log says so.
func (p *Plugin) notify(app *framework.App, a Alert) {
	limit := p.notifyTimeout
	if limit <= 0 {
		limit = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(p.run, limit)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				app.Logger().Error("telemetry: the alert notifier panicked", "rule", a.Rule.Name, "panic", textsafe.Recovered(r))
			}
		}()
		if err := p.cfg.Notify(ctx, a); err != nil {
			app.Logger().Error("telemetry: the alert was not delivered", "rule", a.Rule.Name, "err", err.Error())
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		select {
		case <-done: // it returned as its context ended
		case <-time.After(time.Second):
			app.Logger().Error("telemetry: the alert notifier did not return when its context ended; it is left running", "rule", a.Rule.Name)
		}
	}
}

// Firing returns the rules firing now, as of the last check.
func (p *Plugin) Firing() []Alert {
	alerts := p.alerts.Load()
	if alerts == nil {
		return nil
	}
	var out []Alert
	for _, e := range alerts.Firing() {
		out = append(out, fromEvent(e))
	}
	return out
}

// webhookClient posts alerts: a deadline, and no following a redirect
// to wherever a response points.
var webhookClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// WebhookNotifier returns a Config.Notify that posts each alert to
// target as JSON: {"text": ..., "content": ..., "alert": {...}}. Slack,
// Mattermost and Teams incoming webhooks read "text" and Discord reads
// "content", so one URL of any of them works as it is. A target that
// cannot be posted to is the notifier's error each time it is called.
func WebhookNotifier(target string) func(context.Context, Alert) error {
	u, perr := url.Parse(target)
	return func(ctx context.Context, a Alert) error {
		if perr != nil {
			return errors.New("telemetry: the alert webhook is not a URL")
		}
		body, err := json.Marshal(map[string]any{
			"text": a.Text(), "content": a.Text(),
			"alert": map[string]any{
				"rule": a.Rule.Name, "kind": string(a.Rule.Kind), "route": a.Rule.Route, "firing": a.Firing,
				"count": a.Count, "at_least": a.Rule.AtLeast, "within_seconds": int(a.Rule.Within / time.Second),
				"at": a.At.UTC().Format(time.RFC3339), "since": a.Since.UTC().Format(time.RFC3339),
			},
		})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := webhookClient.Do(req)
		if err != nil {
			// The URL of a chat webhook is its secret: say which host
			// refused, not the whole of it.
			return fmt.Errorf("telemetry: alert webhook to %s failed", u.Host)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("telemetry: alert webhook to %s answered %d", u.Host, resp.StatusCode)
		}
		return nil
	}
}
