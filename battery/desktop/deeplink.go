package desktop

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Deep links: the OS hands the app a URL on its custom scheme
// (`notes://notes/123`), on a cold launch or while running. The shell
// delivers the raw URL through WindowConfig.OnDeepLink; the battery
// validates it, turns it into an app path, navigates the main window,
// and emits a `deep_link` event. Links that arrive before the window is
// up are queued and delivered once it is.

// DeepLinkConfig enables deep links for the app. Zero value: none.
type DeepLinkConfig struct {
	// Scheme is the URL scheme the app claims ("notes"). The bundle
	// builder registers it (CFBundleURLTypes); the battery refuses
	// links on any other scheme.
	Scheme string

	// OnDeepLink, when set, replaces the default mapping: return the
	// app path the link navigates to, or ok=false to drop the link
	// silently. The returned path must pass the same grammar a menu
	// Navigate path passes (leading /, no scheme, no control
	// characters, no . or .. segments) or the link is dropped with a
	// Warn.
	OnDeepLink func(u *url.URL) (path string, ok bool)
}

// Deep-link limits.
const (
	// maxDeepLinkBytes bounds one raw link. The link becomes a log
	// line and a page event; neither should carry an unbounded URL.
	maxDeepLinkBytes = 2048
	// maxDeepLinkSchemeLen bounds the configured scheme (RFC 3986
	// says 2 to 32 for the schemes anyone registers).
	maxDeepLinkSchemeLen = 32
	// maxQueuedDeepLinks bounds the pre-window queue: a cold launch
	// delivers one link, not a stream, and a flooded queue must not
	// grow without bound. Oldest links are dropped first.
	maxQueuedDeepLinks = 16
)

// reDeepLinkScheme is the custom-scheme grammar the battery registers
// (lowercase first letter, then letters, digits, +, ., -).
var reDeepLinkScheme = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)

// validate reports whether the config is usable. A nil config (deep
// links off) is valid.
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

// validateDeepLink is the construction-time check New runs (a wiring
// error you want at construction); handleDeepLink runs it again per
// link so a battery built without New still fails closed.
func validateDeepLink(c *DeepLinkConfig) error { return c.validate() }

// deepLinkPayload is the deep_link event body: what the OS opened and
// where the host navigated.
type deepLinkPayload struct {
	URL  string `json:"url"`
	Path string `json:"path"`
}

// scrubDeepLink makes a raw link safe for a log line: control
// characters become "?" and the copy is bounded. Nothing from the OS
// reaches a log sink unscrubbed.
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

// deepLinkQueue is the per-battery pre-window queue (Battery.deepLinks).
type deepLinkQueue struct {
	mu     sync.Mutex
	queued []string
}

// deepLinkQueueState returns this battery's queue.
func (b *Battery) deepLinkQueueState() *deepLinkQueue {
	return &b.deepLinks
}

// handleDeepLink is WindowConfig.OnDeepLink: the raw URL the OS asked
// the app to open. A link that fails validation is dropped with a Warn
// (scrubbed); a link that arrives before the window is up is queued;
// otherwise it is delivered at once.
func (b *Battery) handleDeepLink(rawURL string) {
	path, ok := b.mapDeepLink(rawURL)
	if !ok {
		return
	}
	w, ok := b.Window()
	if !ok {
		q := b.deepLinkQueueState()
		q.mu.Lock()
		q.queued = append(q.queued, rawURL)
		over := len(q.queued) > maxQueuedDeepLinks
		var dropped string
		if over {
			dropped = q.queued[0]
			q.queued = q.queued[1:]
		}
		q.mu.Unlock()
		if over {
			b.logger.Warn("desktop: deep-link queue is full; dropping the oldest link",
				"url", scrubDeepLink(dropped))
		}
		return
	}
	b.deliverDeepLink(w, rawURL, path)
}

// mapDeepLink validates rawURL against DeepLinkConfig and maps it to an
// app path. ok is false when the link is dropped.
func (b *Battery) mapDeepLink(rawURL string) (string, bool) {
	cfg := b.cfg.DeepLink
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
		// The override owns the mapping; ok=false is the app declining
		// the link, which is not a Warn.
		p, ok := cfg.OnDeepLink(u)
		if !ok {
			return "", false
		}
		path = p
	} else {
		// Default: notes://host/path?q -> /host/path?q. The fragment
		// never crosses (the page owns its own anchors), and the
		// scheme alone maps to the app root.
		path = "/" + u.Host + u.Path
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

// deliverDeepLink focuses the main window, navigates it with the same
// client-side call a menu Navigate item uses, and reports the link to
// every page listener through the deep_link event.
func (b *Battery) deliverDeepLink(w Window, rawURL, path string) {
	if err := w.Focus(); err != nil {
		b.logger.Warn("desktop: focusing the main window for a deep link failed", "error", err.Error())
	}
	pathJSON, err := json.Marshal(path)
	if err != nil {
		b.logger.Error("desktop: deep-link path marshal failed", "error", err.Error())
		return
	}
	js := "window.__gofastr.navigate(" + string(pathJSON) + ")"
	if err := w.Eval(js); err != nil {
		b.logger.Error("desktop: deep-link navigate eval failed", "error", err.Error())
	}
	if err := b.Emit("deep_link", deepLinkPayload{URL: rawURL, Path: path}); err != nil {
		b.logger.Warn("desktop: emitting the deep_link event failed", "error", err.Error())
	}
}

// flushDeepLinks delivers links queued before the window opened, in
// arrival order. Run calls it right after the boot navigation.
func (b *Battery) flushDeepLinks() {
	q := b.deepLinkQueueState()
	q.mu.Lock()
	queued := q.queued
	q.queued = nil
	q.mu.Unlock()
	for _, raw := range queued {
		b.handleDeepLink(raw)
	}
}
