# Unified notifications (`battery/notify`)

`battery/notify` dispatches one domain notification (like `"order.shipped"` or `"password.reset"`) across multiple channels concurrently (`EmailChannel`, `LoggerChannel`, or custom `Channel` adapters for SMS, push, Slack, or in-app feeds).

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/notify"

tmpl := notify.NewMapTemplater()
shipped := notify.Template{
	Subject:  "Order #{{id}} shipped",
	TextBody: "Hi {{name}}, your order is on its way.",
}
tmpl.Set("order.shipped", "email", shipped)
tmpl.Set("order.shipped", "log", shipped)

n := notify.New(
	notify.WithTemplater(tmpl),
	notify.WithChannel(notify.NewLoggerChannel(nil)),
	notify.WithChannel(notify.NewEmailChannel(emailSender, "noreply@example.com")),
	notify.WithErrorCallback(func(ch string, n notify.Notification, err error) {
		log.Printf("notify %s/%s: %v", n.Type, ch, err)
	}),
)

err := n.Send(ctx, notify.Notification{
	Type: "order.shipped",
	To:   notify.Recipient{UserID: "u1", Email: "alice@example.com"},
	Data: map[string]any{"id": 42, "name": "Alice"},
})
```

The default router selects channels from populated `Recipient` fields (`Email` → `email`, `Phone` → `sms`, `Webhook` → `webhook`, `PushTokens` → `push`, plus `log` and `inapp` whenever registered). Override selection (opt-ins, quiet hours, custom channels like `"slack"`) with `notify.WithRouter(...)`.

## Templates, XSS safety, and email extras

- **`HTMLBody` is escaped via `html/template`:** user-supplied values in `Data` are contextually HTML-escaped (neutralizing `javascript:` URLs and tags); opt trusted HTML out explicitly with `template.HTML`. `Subject` strips CR/LF/NUL.
- **`Template.Extra`:** `EmailChannel` forwards `Extra["attachments"]` (`[]email.Attachment`; wrong type returns `notify.ErrInvalidAttachments`) and `Extra["headers"]` (refusing reserved headers). A templated `Extra["from"]` is **rejected** with an error so templates cannot spoof the sender address.
- **Recipient validation:** `EmailChannel` validates addresses with `email.ValidateAddress` (`notify.ErrUnsafeRecipient` on malformed or comma-joined addresses).
- **Localized notifications:** set `Recipient.Locale` and implement `Templater` with `core/i18n` phrases so each notification renders in the recipient's language rather than the caller's.

## Writing a custom channel and error handling

Implement `Name() string` and `Send(ctx, Notification, Rendered) error`. All selected channels fire concurrently under panic recovery; `Send` waits for every channel and returns the first error (while `WithErrorCallback` observes all failures).

## Common mistakes

- **Registering `LoggerChannel` without adding a `"log"` template:** the default router always picks `"log"`, causing `no template` errors on every `Send`.
- **Retrying `n.Send` blindly on partial failure:** other channels may have already succeeded; enqueue per-channel jobs when you need independent retries.
- **Leaving `LoggerChannel` as the only channel in production:** meant for dev/testing (it redacts tokens via `email.RedactBody` and never delivers to users).
- **Forgetting `WithTemplater`:** `Send` returns `ErrNoTemplater` unless you pass pre-rendered `Data["_rendered_*"]`.
