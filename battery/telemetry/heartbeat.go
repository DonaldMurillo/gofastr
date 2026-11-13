package telemetry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// An app cannot say that it is down. What it can do is say, every
// minute, that it is up, to something outside that notices when the
// saying stops: a dead man's switch (Healthchecks, Cronitor, Better
// Stack's heartbeats, a cron monitor of your own). Config.Heartbeat is
// that URL.

// heartbeatClient sends the ping: a deadline, and no following a
// redirect to wherever a response points, since the URL is the secret.
var heartbeatClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// parseHeartbeat checks Config.Heartbeat when the app starts: a typo
// found then is an error, where one found at the first ping would be a
// monitor that never hears from an app that is running.
func parseHeartbeat(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("telemetry: Config.Heartbeat must be an http or https URL")
	}
	return u, nil
}

// beat sends one ping and logs when the pings start failing and when
// they work again: once each way, not once a minute.
func (p *Plugin) beat(app *framework.App) {
	u := p.heartbeat
	if u == nil {
		return
	}
	err := ping(u)
	switch {
	case err != nil && !p.heartbeatDown:
		p.heartbeatDown = true
		app.Logger().Error("telemetry: the heartbeat was not delivered; the monitor will take the app for down", "host", u.Host, "err", err.Error())
	case err == nil && p.heartbeatDown:
		p.heartbeatDown = false
		app.Logger().Info("telemetry: the heartbeat is being delivered again", "host", u.Host)
	}
}

func ping(u *url.URL) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("the URL could not be asked")
	}
	resp, err := heartbeatClient.Do(req)
	if err != nil {
		// Not err's own text: it quotes the URL, which is the secret.
		return errors.New("no answer")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	// A redirect is not a check-in: the monitor at this URL did not
	// take the ping. (The client never returns a 1xx as an answer.)
	if resp.StatusCode > 299 {
		return fmt.Errorf("answered %d", resp.StatusCode)
	}
	return nil
}
