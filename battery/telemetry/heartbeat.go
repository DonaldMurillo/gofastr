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

var heartbeatClient = &http.Client{
	Timeout:       10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

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

func (p *Plugin) beat(app *framework.App) {
	u := p.heartbeat
	if u == nil {
		return
	}
	err := ping(p.run, u)
	if p.run.Err() != nil {
		return
	}
	switch {
	case err != nil && !p.heartbeatDown:
		p.heartbeatDown = true
		app.Logger().Error("telemetry: the heartbeat was not delivered; the monitor will take the app for down", "host", u.Host, "err", err.Error())
	case err == nil && p.heartbeatDown:
		p.heartbeatDown = false
		app.Logger().Info("telemetry: the heartbeat is being delivered again", "host", u.Host)
	}
}

func ping(ctx context.Context, u *url.URL) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("the URL could not be asked")
	}
	resp, err := heartbeatClient.Do(req)
	if err != nil {
		return errors.New("no answer")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode > 299 {
		return fmt.Errorf("answered %d", resp.StatusCode)
	}
	return nil
}
