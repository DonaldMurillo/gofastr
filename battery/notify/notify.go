// Package notify is a small unified-notifications primitive for GoFastr
// apps. Each notification is identified by a type ("order.shipped",
// "password.reset", etc.) and a Recipient; the Notifier renders a
// per-channel template and fans the rendered message out across every
// channel the routing function selects.
//
// Channels are bundled separately so apps can wire whichever subset
// they need (email, in-app, webhook, push) without pulling in
// unrelated dependencies. A LoggerChannel ships in the package for
// development; the email channel adapter lives in this same package
// because [battery/email] is already a framework dependency.
//
// Wiring:
//
//	tmpl := notify.NewMapTemplater()
//	tmpl.Set("order.shipped", "email", notify.Template{
//	    Subject:  "Your order has shipped",
//	    TextBody: "Hi {{name}}, your order #{{id}} is on its way.",
//	})
//	n := notify.New(
//	    notify.WithTemplater(tmpl),
//	    notify.WithChannel(notify.NewLoggerChannel(log.Default())),
//	)
//	err := n.Send(ctx, notify.Notification{
//	    Type: "order.shipped",
//	    To:   notify.Recipient{UserID: "u1", Email: "alice@example.com"},
//	    Data: map[string]any{"name": "Alice", "id": 42},
//	})
package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
)

// Notification is one event-shaped message bound for a single
// Recipient. The Notifier picks channels by inspecting the Recipient
// (e.g. only sends to "email" channel when To.Email is non-empty),
// renders a per-channel template against Data, and fans out.
type Notification struct {
	Type string
	To   Recipient
	Data map[string]any
}

// Recipient is the destination set. Channels are responsible for
// reading the fields they care about — UserID is canonical, the rest
// are channel-specific addresses.
type Recipient struct {
	UserID  string
	Email   string
	Phone   string
	Webhook string
	// PushTokens is the optional list of device tokens for push
	// channels — kept as a slice rather than a single field because
	// users frequently have multiple devices.
	PushTokens []string
}

// Rendered is the per-channel materialised message.
type Rendered struct {
	Subject  string
	TextBody string
	HTMLBody string
	// Extra carries channel-specific extras the templater wants the
	// channel to see — e.g. an attachment list, a SMS short-link, a
	// webhook payload override.
	Extra map[string]any
}

// Channel is the send-side adapter. Implementations must be safe for
// concurrent use; the Notifier may invoke Send from many goroutines.
type Channel interface {
	Name() string
	Send(ctx context.Context, n Notification, r Rendered) error
}

// Templater renders a Notification + channel name into a Rendered
// payload. Implementations should be deterministic over the
// (notifType, channel, data) inputs — caching is the caller's choice.
type Templater interface {
	Render(ctx context.Context, notifType, channel string, data map[string]any) (Rendered, error)
}

// Router decides which channels a Notification should target. Default
// router: every registered channel that "applies" to the Recipient.
// Channels declare their applicability by name; for built-ins:
//
//   - "email" applies when Recipient.Email != ""
//   - "sms" applies when Recipient.Phone != ""
//   - "webhook" applies when Recipient.Webhook != ""
//   - "push" applies when len(Recipient.PushTokens) > 0
//   - "log" / "inapp" always apply
type Router func(notifType string, to Recipient, channels []string) []string

// Notifier is the front door. Construct with [New], register channels
// via [WithChannel], and call [Send] from handlers.
type Notifier struct {
	mu        sync.RWMutex
	channels  map[string]Channel
	chanOrder []string
	templater Templater
	router    Router
	onError   func(channel string, n Notification, err error)
}

// Option configures the Notifier.
type Option func(*Notifier)

// WithTemplater installs a Templater. If unset, [Notifier.Send]
// returns [ErrNoTemplater] for any notification that doesn't carry a
// pre-rendered payload in Data["_rendered"].
func WithTemplater(t Templater) Option {
	return func(n *Notifier) { n.templater = t }
}

// WithChannel registers a Channel. Multiple calls accumulate; channel
// names must be unique (a second registration replaces the first).
func WithChannel(c Channel) Option {
	return func(n *Notifier) {
		if _, exists := n.channels[c.Name()]; !exists {
			n.chanOrder = append(n.chanOrder, c.Name())
		}
		n.channels[c.Name()] = c
	}
}

// WithRouter installs a custom router. The default router uses
// DefaultRouter which selects channels by Recipient field presence.
func WithRouter(r Router) Option {
	return func(n *Notifier) { n.router = r }
}

// WithErrorCallback installs a callback for per-channel send errors.
// The Notifier still attempts every selected channel; the callback
// surfaces failures so they can be logged/metricized without bringing
// the call site down.
func WithErrorCallback(fn func(channel string, n Notification, err error)) Option {
	return func(n *Notifier) { n.onError = fn }
}

// Errors returned by Send.
var (
	ErrNoTemplater = errors.New("notify: no templater installed")
	ErrNoChannels  = errors.New("notify: no channels selected for notification")
)

// New constructs a Notifier with the supplied options.
func New(opts ...Option) *Notifier {
	n := &Notifier{
		channels: map[string]Channel{},
		router:   DefaultRouter,
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
}

// Send routes the notification, renders it per channel, and fires
// each channel concurrently. Returns the first error encountered (or
// nil); use WithErrorCallback to observe per-channel failures.
//
// Notifications with no selected channel return ErrNoChannels.
// Notifications without a templater (and no pre-rendered payload in
// Data["_rendered"]) return ErrNoTemplater.
func (n *Notifier) Send(ctx context.Context, msg Notification) error {
	n.mu.RLock()
	chanNames := append([]string(nil), n.chanOrder...)
	channels := make(map[string]Channel, len(n.channels))
	for k, v := range n.channels {
		channels[k] = v
	}
	router := n.router
	templater := n.templater
	onError := n.onError
	n.mu.RUnlock()

	selected := router(msg.Type, msg.To, chanNames)
	if len(selected) == 0 {
		return ErrNoChannels
	}

	// Render+send each channel concurrently. A pre-rendered payload
	// keyed under Data["_rendered_<channel>"] short-circuits the
	// templater for that channel; useful for tests and for callers
	// that already know the body.
	type result struct {
		channel string
		err     error
	}
	out := make(chan result, len(selected))
	var wg sync.WaitGroup
	for _, name := range selected {
		ch, ok := channels[name]
		if !ok {
			continue
		}
		wg.Add(1)
		go func(name string, ch Channel) {
			defer wg.Done()
			rendered, err := n.renderFor(ctx, templater, msg, name)
			if err != nil {
				if onError != nil {
					onError(name, msg, err)
				}
				out <- result{channel: name, err: err}
				return
			}
			if err := ch.Send(ctx, msg, rendered); err != nil {
				if onError != nil {
					onError(name, msg, err)
				}
				out <- result{channel: name, err: err}
				return
			}
			out <- result{channel: name}
		}(name, ch)
	}
	wg.Wait()
	close(out)

	var firstErr error
	for r := range out {
		if r.err != nil && firstErr == nil {
			firstErr = fmt.Errorf("notify: channel %q: %w", r.channel, r.err)
		}
	}
	return firstErr
}

func (n *Notifier) renderFor(ctx context.Context, t Templater, msg Notification, channel string) (Rendered, error) {
	if msg.Data != nil {
		// Per-channel pre-rendered payload short-circuit.
		if pre, ok := msg.Data["_rendered_"+channel].(Rendered); ok {
			return pre, nil
		}
		if pre, ok := msg.Data["_rendered"].(Rendered); ok {
			return pre, nil
		}
	}
	if t == nil {
		return Rendered{}, ErrNoTemplater
	}
	return t.Render(ctx, msg.Type, channel, msg.Data)
}

// Channels returns the registered channel names in registration order.
func (n *Notifier) Channels() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]string(nil), n.chanOrder...)
}

// DefaultRouter selects channels by Recipient field presence:
//
//   - "email"   when To.Email != ""
//   - "sms"     when To.Phone != ""
//   - "webhook" when To.Webhook != ""
//   - "push"    when len(To.PushTokens) > 0
//   - "log" / "inapp" always
//
// Channels with names not in this list are skipped — register a
// custom router via WithRouter to support more.
func DefaultRouter(_ string, to Recipient, channels []string) []string {
	var out []string
	for _, c := range channels {
		switch c {
		case "email":
			if to.Email != "" {
				out = append(out, c)
			}
		case "sms":
			if to.Phone != "" {
				out = append(out, c)
			}
		case "webhook":
			if to.Webhook != "" {
				out = append(out, c)
			}
		case "push":
			if len(to.PushTokens) > 0 {
				out = append(out, c)
			}
		case "log", "inapp":
			out = append(out, c)
		}
	}
	return out
}

// ----- bundled MapTemplater ------------------------------------------------

// Template is one per-channel template. The fields are interpolated
// with the same `{{placeholder}}` form used by the i18n package.
type Template struct {
	Subject  string
	TextBody string
	HTMLBody string
	Extra    map[string]any
}

// MapTemplater is the simplest Templater: a (notifType, channel) →
// Template lookup table. Suitable for apps with a small fixed set of
// notifications; for catalog-driven i18n use [I18nTemplater].
type MapTemplater struct {
	mu        sync.RWMutex
	templates map[string]map[string]Template // type → channel → template
}

// NewMapTemplater returns an empty MapTemplater.
func NewMapTemplater() *MapTemplater {
	return &MapTemplater{templates: map[string]map[string]Template{}}
}

// Set registers a template for the (notifType, channel) pair.
func (m *MapTemplater) Set(notifType, channel string, t Template) {
	m.mu.Lock()
	if m.templates[notifType] == nil {
		m.templates[notifType] = map[string]Template{}
	}
	m.templates[notifType][channel] = t
	m.mu.Unlock()
}

// Render implements Templater.
//
// The rendered Subject is stripped of CR / LF / NUL so a user-controlled
// {{placeholder}} can't inject header continuations when downstream
// transports (SMTP, push providers) treat Subject as a header value.
// TextBody / HTMLBody are not modified — those are payload bytes and
// any HTML safety is the rendering layer's job.
func (m *MapTemplater) Render(_ context.Context, notifType, channel string, data map[string]any) (Rendered, error) {
	m.mu.RLock()
	t, ok := m.templates[notifType][channel]
	m.mu.RUnlock()
	if !ok {
		return Rendered{}, fmt.Errorf("notify: no template for (%q, %q)", notifType, channel)
	}
	return Rendered{
		Subject:  stripHeaderUnsafe(interpolate(t.Subject, data)),
		TextBody: interpolate(t.TextBody, data),
		HTMLBody: interpolate(t.HTMLBody, data),
		Extra:    t.Extra,
	}, nil
}

// stripHeaderUnsafe drops the three bytes that turn a single-line
// header value into a multi-line injection vector (CR, LF, NUL).
func stripHeaderUnsafe(s string) string {
	if !strings.ContainsAny(s, "\r\n\x00") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\r', '\n', 0x00:
			continue
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// MaxInterpolatedOutputBytes is the hard cap on the output of a single
// [interpolate] call. A 10 MB placeholder value would otherwise produce
// an unbounded subject / body string and pin a goroutine in fmt.Fprint.
// 1 MiB is well above any legitimate notification payload.
const MaxInterpolatedOutputBytes = 1 << 20

// interpolate is a local copy of the i18n placeholder expander (kept
// inline so this package has no dependency on core/i18n). Unknown
// placeholders are left intact for visibility during development.
//
// Output is hard-capped at MaxInterpolatedOutputBytes — a giant
// placeholder value would otherwise produce an unbounded string and
// pin the rendering goroutine. The cap is far above any legitimate
// notification size, so triggering it indicates abuse.
func interpolate(s string, params map[string]any) string {
	if s == "" || len(params) == 0 || !strings.Contains(s, "{{") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	write := func(chunk string) bool {
		if b.Len()+len(chunk) > MaxInterpolatedOutputBytes {
			b.WriteString(chunk[:MaxInterpolatedOutputBytes-b.Len()])
			return false
		}
		b.WriteString(chunk)
		return true
	}
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], "{{")
		if j < 0 {
			write(s[i:])
			break
		}
		if !write(s[i : i+j]) {
			break
		}
		k := strings.Index(s[i+j+2:], "}}")
		if k < 0 {
			write(s[i+j:])
			break
		}
		name := strings.TrimSpace(s[i+j+2 : i+j+2+k])
		if v, ok := params[name]; ok {
			if !write(fmt.Sprint(v)) {
				break
			}
		} else {
			if !write(s[i+j : i+j+2+k+2]) {
				break
			}
		}
		i = i + j + 2 + k + 2
	}
	return b.String()
}

// ----- bundled LoggerChannel -----------------------------------------------

// LoggerChannel writes notifications to a *log.Logger — useful for
// development and CI. Always applies (DefaultRouter routes "log" or
// "inapp" unconditionally).
type LoggerChannel struct {
	name string
	log  *log.Logger
}

// NewLoggerChannel constructs a logger-backed channel. Pass nil to
// use the default logger; pass "" name for "log".
func NewLoggerChannel(l *log.Logger) *LoggerChannel {
	if l == nil {
		l = log.Default()
	}
	return &LoggerChannel{name: "log", log: l}
}

// Name returns "log".
func (c *LoggerChannel) Name() string { return c.name }

// Send writes a one-line record to the logger.
func (c *LoggerChannel) Send(_ context.Context, n Notification, r Rendered) error {
	c.log.Printf("[notify] type=%s to_user=%q to_email=%q subject=%q text=%q",
		n.Type, n.To.UserID, n.To.Email, r.Subject, r.TextBody)
	return nil
}
