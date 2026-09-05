# Unified notifications

`battery/notify` sends one notification through several channels at
once: each notification has a type ("order.shipped", "password.reset"),
a Recipient, and a free-form Data map. The Notifier renders a
per-channel template against Data and fires every applicable channel
concurrently.

It's deliberately small. Each channel is its own adapter; the
package bundles `LoggerChannel` (dev) and `EmailChannel` (wrapping
`battery/email`), and any third-party channel (in-app feed, SMS,
push, Slack, …) just implements `Channel`.

## Wiring

```go
import (
    "github.com/DonaldMurillo/gofastr/battery/notify"
    "github.com/DonaldMurillo/gofastr/battery/email"
)

tmpl := notify.NewMapTemplater()
tmpl.Set("order.shipped", "email", notify.Template{
    Subject:  "Order #{{id}} shipped",
    TextBody: "Hi {{name}}, your order is on its way.",
})

n := notify.New(
    notify.WithTemplater(tmpl),
    notify.WithChannel(notify.NewLoggerChannel(nil)),
    notify.WithChannel(notify.NewEmailChannel(emailSender, "noreply@example.com")),
    notify.WithErrorCallback(func(ch string, n notify.Notification, err error) {
        log.Printf("notify %s/%s: %v", n.Type, ch, err)
    }),
)

// later, from a handler
err := n.Send(ctx, notify.Notification{
    Type: "order.shipped",
    To:   notify.Recipient{UserID: "u1", Email: "alice@example.com"},
    Data: map[string]any{"id": 42, "name": "Alice"},
})
```

`Send` renders and fires every channel the router selects concurrently.
The default router selects by Recipient field:

| Channel name | Selected when                     |
|--------------|-----------------------------------|
| `email`      | `To.Email != ""`                  |
| `sms`        | `To.Phone != ""`                  |
| `webhook`    | `To.Webhook != ""`                |
| `push`       | `len(To.PushTokens) > 0`          |
| `log`        | always                            |
| `inapp`      | always                            |

Override with `notify.WithRouter(...)` for app-specific logic
(quiet hours, opt-ins, channel preference per user).

## Templates

`MapTemplater` is the bundled in-memory templater: an explicit
(notifType, channel) lookup table. Both fields are interpolated
with the same `{{placeholder}}` form used by `core/i18n`; unknown
placeholders are left intact so they're visible during development.

```go
tmpl.Set("password.reset", "email", notify.Template{
    Subject:  "Reset your password",
    TextBody: "Open this link: {{link}}",
    HTMLBody: `<a href="{{link}}">Reset password</a>`,
})
```

**`HTMLBody` values are HTML-escaped at the interpolation sink.** Data
values are routinely user-controlled (display names, addresses,
user-entered content), and the bundled `EmailChannel` hands
`Rendered.HTMLBody` straight to the SMTP HTML part — webmail is an HTML
application, so an unescaped value is a session-theft-class XSS. The
template bytes themselves are yours and pass through untouched; to opt
ONE value out (markup you generated), type it `html/template.HTML`:

```go
data := map[string]any{
    "name":  user.Name, // escaped
    "badge": template.HTML(badgeMarkup), // trusted markup, verbatim
}
```

`Subject` and `TextBody` are payload bytes and are not HTML-escaped
(Subject still has CR/LF/NUL stripped so a placeholder can't inject
header continuations). A custom `Templater` owns the same obligation:
`EmailChannel` passes `Rendered.HTMLBody` through verbatim.

`Template.Extra` is passed through to the channel as `Rendered.Extra`:
the bundled `EmailChannel` honours `Extra["headers"]` (custom headers,
with reserved keys like To/From/Cc/Bcc/Reply-To rejected) so
per-notification overrides don't need a custom Channel. A templated
`Extra["from"]` is **rejected** with an error: a per-notification sender
override is a spoofing vector, so the channel's configured from-address
is the only legitimate source.

For larger apps with translations, render templates yourself via the
`core/i18n` package and pass the result via `Data["_rendered_email"]`
/ `Data["_rendered"]`; the Notifier short-circuits the templater for
that channel.

## Writing a channel

```go
type SlackChannel struct{ webhookURL string }

func (s SlackChannel) Name() string { return "slack" }

func (s SlackChannel) Send(ctx context.Context, n notify.Notification, r notify.Rendered) error {
    body, _ := json.Marshal(map[string]any{"text": r.TextBody})
    req, _ := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode >= 300 { return fmt.Errorf("slack status %d", resp.StatusCode) }
    return nil
}

// Register
n := notify.New(notify.WithChannel(SlackChannel{webhookURL: url}))
```

If the default router doesn't know about your channel name, register
a custom router via `WithRouter` (or just always include it).

## Error handling

`Send` returns the first per-channel error wrapped as
`notify: channel %q: <err>`. Use `WithErrorCallback` to observe every
channel's outcome, useful when you want a green path even if one
channel breaks, but want metrics / alerts when something does.

Channels are fired concurrently; a slow channel doesn't block the
others, and the call returns only after every channel has finished.

Channels, templaters, and the error callback are host-supplied code, so
every invocation runs under a recover guard: a panicking `Send`, `Render`,
or error callback surfaces as an attributed error for that channel (and is
still delivered to `WithErrorCallback`) instead of crashing the process.

## Common mistakes

- **Don't conflate Type with a free-form subject.** Type is the
  symbol your templater keys on; user-facing text lives in the
  Template / Rendered fields.
- **Don't bundle large payloads in Data.** Anything that ends up in
  Rendered.TextBody / HTMLBody gets pushed across every channel; for
  attachments use channel-specific Extra entries or pre-render.
- **Don't rely on the LoggerChannel in production.** It writes to a
  *log.Logger, meant for development, CI, and as a sanity sink. Like
  `email.LogSender`, it redacts credential-bearing URLs (`?token=…`,
  `?code=…`, and the other secret query params) and `Bearer` values
  from the rendered subject and text body before writing, via
  `email.RedactBody`, so a reset-link notification never lands
  token-first in the log.
- **Don't forget the templater.** Without one, every `Send` returns
  `ErrNoTemplater` unless you supply `Data["_rendered_*"]` per
  channel.
