// Package notify sends one typed notification through every channel it applies to.
package notify

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"maps"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/battery/email"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// Notification is one event-shaped message bound for a single Recipient.
type Notification struct {
	Type string
	To   Recipient
	Data map[string]any
}

// Recipient is the destination set.
type Recipient struct {
	UserID  string
	Email   string
	Phone   string
	Webhook string
	// PushTokens lists the recipient's push device tokens.
	PushTokens []string
	// Locale is the BCP 47 tag the templater and channel run under; empty keeps ctx's.
	Locale string
}

// Rendered is the per-channel materialised message.
type Rendered struct {
	Subject  string
	TextBody string
	HTMLBody string
	// Extra carries channel-specific values, such as email attachments or headers.
	Extra map[string]any
}

// Channel is the send-side adapter.
type Channel interface {
	Name() string
	Send(ctx context.Context, n Notification, r Rendered) error
}

// Templater renders a notification type for one channel.
type Templater interface {
	Render(ctx context.Context, notifType, channel string, data map[string]any) (Rendered, error)
}

// Router decides which channels a Notification should target.
type Router func(notifType string, to Recipient, channels []string) []string

// Notifier is the front door.
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

// WithTemplater installs a Templater.
func WithTemplater(t Templater) Option {
	return func(n *Notifier) { n.templater = t }
}

// WithChannel registers a Channel.
func WithChannel(c Channel) Option {
	return func(n *Notifier) {
		if _, exists := n.channels[c.Name()]; !exists {
			n.chanOrder = append(n.chanOrder, c.Name())
		}
		n.channels[c.Name()] = c
	}
}

// WithRouter installs a custom router.
func WithRouter(r Router) Option {
	return func(n *Notifier) { n.router = r }
}

// WithErrorCallback installs a callback for per-channel send errors.
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

// Send renders msg per routed channel and sends concurrently, returning the first error: ErrNoChannels
// when none is routed, ErrNoTemplater for a channel with no templater or Rendered payload.
func (n *Notifier) Send(ctx context.Context, msg Notification) error {
	n.mu.RLock()
	chanNames := append([]string(nil), n.chanOrder...)
	channels := make(map[string]Channel, len(n.channels))
	maps.Copy(channels, n.channels)
	router := n.router
	templater := n.templater
	onError := n.onError
	n.mu.RUnlock()

	ctx = recipientContext(ctx, msg.To)
	selected, rerr := routeSafe(router, msg, chanNames)
	if rerr != nil {
		return rerr
	}
	resolved := make([]string, 0, len(selected))
	seen := make(map[string]bool, len(selected))
	for _, name := range selected {
		if _, ok := channels[name]; ok && !seen[name] {
			resolved = append(resolved, name)
			seen[name] = true
		}
	}
	selected = resolved
	if len(selected) == 0 {
		return ErrNoChannels
	}

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
			if err == nil {
				err = channelSendSafely(ctx, ch, msg, rendered)
			}
			if err != nil {
				if cbErr := notifyOnErrorSafely(onError, name, msg, err); cbErr != nil {
					err = cbErr
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

func routeSafe(router Router, msg Notification, chanNames []string) (selected []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("notify: router panic: %v", r)
		}
	}()
	return router(msg.Type, msg.To, chanNames), nil
}

func recipientContext(ctx context.Context, to Recipient) context.Context {
	if to.Locale == "" {
		return ctx
	}
	tr := i18n.TranslatorFrom(ctx)
	if tr == nil {
		tr = i18n.Default()
	}
	loc, _ := i18n.Match(tr, to.Locale)
	return i18n.WithContext(ctx, loc)
}

func (n *Notifier) renderFor(ctx context.Context, t Templater, msg Notification, channel string) (r Rendered, err error) {
	defer func() {
		if v := recover(); v != nil {
			r, err = Rendered{}, fmt.Errorf("notify: templater panicked: %v", v)
		}
	}()
	for _, key := range []string{"_rendered_" + channel, "_rendered"} {
		value, present := msg.Data[key]
		if !present {
			continue
		}
		pre, ok := value.(Rendered)
		if !ok {
			return Rendered{}, fmt.Errorf("notify: %s must be notify.Rendered, got %T", key, value)
		}
		return pre, nil
	}
	if t == nil {
		return Rendered{}, ErrNoTemplater
	}
	return t.Render(ctx, msg.Type, channel, msg.Data)
}

func channelSendSafely(ctx context.Context, ch Channel, msg Notification, r Rendered) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("notify: channel send panicked: %v", v)
		}
	}()
	return ch.Send(ctx, msg, r)
}

func notifyOnErrorSafely(onError func(channel string, n Notification, err error), name string, msg Notification, err error) (cbErr error) {
	if onError == nil {
		return nil
	}
	defer func() {
		if v := recover(); v != nil {
			cbErr = fmt.Errorf("notify: error callback for channel %q panicked: %v", name, v)
		}
	}()
	onError(name, msg, err)
	return nil
}

// Channels returns the registered channel names in registration order.
func (n *Notifier) Channels() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]string(nil), n.chanOrder...)
}

// DefaultRouter selects email, sms, webhook and push when the Recipient
// has that address, and log and inapp always.
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

// Template is one per-channel template.
type Template struct {
	Subject  string
	TextBody string
	HTMLBody string
	Extra    map[string]any
}

// MapTemplater is a Templater over a (notifType, channel) table.
type MapTemplater struct {
	mu        sync.RWMutex
	templates map[string]map[string]Template
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
	t.Extra = maps.Clone(t.Extra)
	m.templates[notifType][channel] = t
	m.mu.Unlock()
}

// Render fills the (notifType, channel) template with data, escaping
// HTMLBody values for their HTML context.
func (m *MapTemplater) Render(_ context.Context, notifType, channel string, data map[string]any) (Rendered, error) {
	m.mu.RLock()
	t, ok := m.templates[notifType][channel]
	m.mu.RUnlock()
	if !ok {
		return Rendered{}, fmt.Errorf("notify: no template for (%q, %q)", notifType, channel)
	}
	html, err := renderHTMLBody(t.HTMLBody, data)
	if err != nil {
		return Rendered{}, err
	}
	return Rendered{
		Subject:  stripHeaderUnsafe(interpolate(t.Subject, data)),
		TextBody: interpolate(t.TextBody, data),
		HTMLBody: html,
		Extra:    maps.Clone(t.Extra),
	}, nil
}

func stripHeaderUnsafe(s string) string {
	if !strings.ContainsFunc(s, textsafe.IsUnsafe) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if textsafe.IsUnsafe(r) {
			return -1
		}
		return r
	}, s)
}

// MaxInterpolatedOutputBytes caps one rendered field.
const MaxInterpolatedOutputBytes = 1 << 20

func interpolate(s string, params map[string]any) string {
	return interpolateValues(s, params, nil)
}

func interpolateValues(s string, params map[string]any, format func(any) string) string {
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
			if format != nil {
				if !write(format(v)) {
					break
				}
			} else if !write(fmt.Sprint(v)) {
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

const htmlDelimL, htmlDelimR = "\x00[", "]\x00"

func renderHTMLBody(body string, data map[string]any) (string, error) {
	if body == "" || len(data) == 0 || !strings.Contains(body, "{{") {
		return body, nil
	}
	if strings.IndexByte(body, 0) >= 0 {
		return "", errors.New("notify: HTMLBody holds a NUL byte")
	}
	var vals []any
	src := interpolateValues(body, data, func(v any) string {
		vals = append(vals, v)
		return htmlDelimL + "index . " + strconv.Itoa(len(vals)-1) + htmlDelimR
	})
	t, err := template.New("html").Delims(htmlDelimL, htmlDelimR).Parse(src)
	if err != nil {
		return "", fmt.Errorf("notify: HTMLBody: %w", err)
	}
	w := &cappedWriter{max: MaxInterpolatedOutputBytes}
	if err := t.Execute(w, vals); err != nil {
		return "", fmt.Errorf("notify: HTMLBody: %w", err)
	}
	return w.b.String(), nil
}

type cappedWriter struct {
	b   strings.Builder
	max int
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if room := w.max - w.b.Len(); room > 0 {
		w.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// LoggerChannel writes notifications to a *log.Logger, for development.
type LoggerChannel struct {
	name string
	log  *log.Logger
}

// NewLoggerChannel constructs a logger-backed channel.
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
		n.Type, n.To.UserID, n.To.Email, email.RedactBody(r.Subject), email.RedactBody(r.TextBody))
	return nil
}
