# battery/notify

Unified notification primitive. Each notification has a `Type`
(`"password.reset"`, `"order.shipped"`, …) and a `Recipient`; the
Notifier renders per-channel templates and fans them out across the
channels the routing function selects.

**Use this when** the prompt mentions: send notification, email user,
in-app notice, password reset link, "send a magic link", webhook
trigger, "tell the user X happened".

**Import:** `github.com/DonaldMurillo/gofastr/battery/notify`

**Shape (dev — log only):**
```go
tmpl := notify.NewMapTemplater()
tmpl.Set("password.reset", "log", notify.Template{
    Subject:  "Reset your password",
    TextBody: "Open {{url}} within 15 minutes.",
})
n := notify.New(
    notify.WithTemplater(tmpl),
    notify.WithChannel(notify.NewLoggerChannel(log.Default())),
)
_ = n.Send(ctx, notify.Notification{
    Type: "password.reset",
    To:   notify.Recipient{UserID: u.ID, Email: u.Email},
    Data: map[string]any{"url": resetURL},
})
```

**Shape (prod — email):**
```go
emailCh := notify.NewEmailChannel(emailSender, "noreply@example.com")
n := notify.New(notify.WithTemplater(tmpl), notify.WithChannel(emailCh))
```

**Don't reinvent** password-reset URL printing, magic-link sending,
admin "your job finished" notices, or webhook fan-out. The same code
path swaps `LoggerChannel` → `EmailChannel` → `WebhookChannel` without
changing the call site.

**For PHI-bearing apps:** templates are rendered with `Data` you pass
in — don't put PHI in `Data` if the channel writes to a long-lived
sink (logs, webhook receivers you don't control).
