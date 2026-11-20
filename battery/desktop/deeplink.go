package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// DeepLinkConfig enables deep links for the app.
type DeepLinkConfig struct {
	// Scheme is the URL scheme the app claims ("notes").
	Scheme string

	// OnDeepLink, when set, maps a link to its app path, or returns ok=false to drop it.
	OnDeepLink func(u *url.URL) (path string, ok bool)
}

const (
	maxDeepLinkBytes     = 2048
	maxDeepLinkSchemeLen = 32
	maxQueuedDeepLinks   = 16
)

var reDeepLinkScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

func (c *DeepLinkConfig) validate() error {
	if c == nil {
		return nil
	}
	if c.Scheme == "" {
		return fmt.Errorf("desktop: DeepLink.Scheme is required when Config.DeepLink is set")
	}
	if len(c.Scheme) > maxDeepLinkSchemeLen {
		return fmt.Errorf("desktop: DeepLink.Scheme %q is longer than %d characters", c.Scheme, maxDeepLinkSchemeLen)
	}
	if !reDeepLinkScheme.MatchString(c.Scheme) {
		return fmt.Errorf("desktop: DeepLink.Scheme %q must match %s", c.Scheme, reDeepLinkScheme.String())
	}
	return nil
}

func validateDeepLink(c *DeepLinkConfig) error { return c.validate() }

type deepLinkPayload struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

func scrubDeepLink(raw string) string {
	const max = 200
	if len(raw) > max {
		raw = raw[:max]
	}
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			r = '?'
		}
		b.WriteRune(r)
	}
	return b.String()
}

type deepLinkQueue struct {
	mu       sync.Mutex
	queued   []queuedDeepLink
	pending  []deepLinkDelivery
	draining bool
	flushed  bool
}

var deepLinkRouteHook atomic.Pointer[func()]

type deepLinkDelivery struct {
	w         Window
	raw, path string
}

type queuedDeepLink struct {
	raw  string
	path string
}

func (b *Battery) deepLinkQueueState() *deepLinkQueue {
	return &b.deepLinks
}

func (b *Battery) handleDeepLink(rawURL string) {
	path, ok := b.mapDeepLink(rawURL)
	if !ok {
		return
	}
	q := b.deepLinkQueueState()
	q.mu.Lock()
	w, up := b.Window()
	if hook := deepLinkRouteHook.Load(); hook != nil {
		(*hook)()
	}
	if up && q.flushed {
		start, dropped := q.pushLocked(deepLinkDelivery{w: w, raw: rawURL, path: path})
		q.mu.Unlock()
		b.startDeepLinkDrain(start, dropped)
		return
	}
	q.queued = append(q.queued, queuedDeepLink{raw: rawURL, path: path})
	over := len(q.queued) > maxQueuedDeepLinks
	var dropped string
	if over {
		dropped = q.queued[0].raw
		q.queued = q.queued[1:]
	}
	q.mu.Unlock()
	if over {
		b.logger.Warn("desktop: deep-link queue is full; dropping the oldest link",
			"url", scrubDeepLink(dropped))
	}
}

func (b *Battery) mapDeepLink(rawURL string) (string, bool) {
	cfg := b.cfg.DeepLink
	if cfg == nil {
		b.logger.Warn("desktop: dropping a deep link with deep links off", "url", scrubDeepLink(rawURL))
		return "", false
	}
	if err := validateDeepLink(cfg); err != nil {
		b.logger.Warn("desktop: dropping a deep link with no valid DeepLink config",
			"reason", err.Error(), "url", scrubDeepLink(rawURL))
		return "", false
	}
	if len(rawURL) > maxDeepLinkBytes {
		b.logger.Warn("desktop: dropping a deep link longer than the byte bound",
			"length", len(rawURL), "bound", maxDeepLinkBytes)
		return "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		b.logger.Warn("desktop: dropping an unparseable deep link", "url", scrubDeepLink(rawURL))
		return "", false
	}
	if !strings.EqualFold(u.Scheme, cfg.Scheme) {
		b.logger.Warn("desktop: dropping a deep link on a refused scheme", "url", scrubDeepLink(rawURL))
		return "", false
	}
	if u.User != nil {
		b.logger.Warn("desktop: dropping a deep link carrying userinfo", "url", scrubDeepLink(rawURL))
		return "", false
	}
	var path string
	if cfg.OnDeepLink != nil {
		p, ok := cfg.OnDeepLink(u)
		if !ok {
			return "", false
		}
		path = p
	} else {
		path = "/" + u.Host + u.EscapedPath()
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
	}
	if !validNavigatePath(path) {
		b.logger.Warn("desktop: dropping a deep link whose path fails the navigate grammar",
			"url", scrubDeepLink(rawURL), "path", scrubDeepLink(path))
		return "", false
	}
	return path, true
}

func (b *Battery) enqueueDeepLink(d deepLinkDelivery) {
	q := b.deepLinkQueueState()
	q.mu.Lock()
	start, dropped := q.pushLocked(d)
	q.mu.Unlock()
	b.startDeepLinkDrain(start, dropped)
}

func (q *deepLinkQueue) pushLocked(d deepLinkDelivery) (start bool, dropped []string) {
	q.pending = append(q.pending, d)
	if len(q.pending) > maxQueuedDeepLinks {
		dropped = append(dropped, q.pending[0].raw)
		q.pending = q.pending[1:]
	}
	start = !q.draining
	q.draining = true
	return start, dropped
}

func (b *Battery) startDeepLinkDrain(start bool, dropped []string) {
	for _, raw := range dropped {
		b.logger.Warn("desktop: deep-link delivery queue is full; dropping the oldest pending link",
			"url", scrubDeepLink(raw))
	}
	if start {
		go b.drainDeepLinks()
	}
}

func (b *Battery) drainDeepLinks() {
	q := b.deepLinkQueueState()
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.draining = false
			q.mu.Unlock()
			return
		}
		d := q.pending[0]
		q.pending = q.pending[1:]
		q.mu.Unlock()
		b.deliverDeepLink(d.w, d.raw, d.path)
	}
}

const deepLinkPageWait = 15 * time.Second

func (b *Battery) awaitPageRuntime(w Window) {
	pe, ok := w.(PageEvaluator)
	if !ok {
		return
	}
	deadline := time.Now().Add(deepLinkPageWait)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		r, err := pe.EvalAsync(ctx, "return typeof (window.__gofastr && window.__gofastr.navigate) === 'function'")
		cancel()
		if err == nil && string(r) == "true" {
			return
		}
		if time.Now().After(deadline) {
			b.logger.Warn("desktop: the page's runtime did not appear before the deep link was delivered",
				"waited", deepLinkPageWait.String())
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (b *Battery) deliverDeepLink(w Window, rawURL, path string) {
	defer func() {
		if v := recover(); v != nil {
			b.logger.Error("desktop: deep-link delivery panicked", "path", path, "panic", textsafe.Recovered(v))
		}
	}()
	if err := w.Focus(); err != nil {
		b.logger.Warn("desktop: focusing the main window for a deep link failed", "error", err.Error())
	}
	b.awaitPageRuntime(w)
	pathJSON, err := json.Marshal(path)
	if err != nil {
		b.logger.Error("desktop: deep-link path marshal failed", "error", err.Error())
		return
	}
	js := "window.__gofastr.navigate(" + string(pathJSON) + ")"
	if err := w.Eval(b.inAppJS(js)); err != nil {
		b.logger.Error("desktop: deep-link navigate eval failed", "error", err.Error())
	}
	if err := b.Emit("deep_link", deepLinkPayload{URL: rawURL, Path: path}); err != nil {
		b.logger.Warn("desktop: emitting the deep_link event failed", "error", err.Error())
	}
}

func (b *Battery) flushDeepLinks(w Window) {
	q := b.deepLinkQueueState()
	q.mu.Lock()
	queued := q.queued
	q.queued = nil
	q.flushed = true
	start := false
	var dropped []string
	for _, l := range queued {
		s, d := q.pushLocked(deepLinkDelivery{w: w, raw: l.raw, path: l.path})
		start = start || s
		dropped = append(dropped, d...)
	}
	q.mu.Unlock()
	b.startDeepLinkDrain(start, dropped)
}
