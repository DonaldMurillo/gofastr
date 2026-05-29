# battery/email

Transactional email through a `Sender` interface. Ships a SMTP sender,
a stderr log sender for dev, and a tiny template loader.

**Use this when** the prompt mentions: send email, transactional
email, SMTP, "email the user", invite email, "welcome email",
HTML+plaintext message.

**Import:** `github.com/DonaldMurillo/gofastr/battery/email`

**Shape (dev — write to stderr):**
```go
s := email.NewLogSender(os.Stderr)
_ = s.Send(ctx, email.Email{
    To:       []string{"alice@example.com"},
    Subject:  "Welcome",
    TextBody: "Hi {{.Name}}, you're in.",
})
```

**Shape (prod — SMTP):**
```go
s, err := email.NewSMTPSender(email.SMTPConfig{
    Host: "smtp.example.com", Port: 587,
    Username: u, Password: p,
    From: "noreply@example.com",
    TLS:  true,
})
```

**Transport encryption (fail-closed):** when `UseTLS` is false the sender
attempts opportunistic `STARTTLS`. If the server does not advertise
STARTTLS (e.g. an on-path downgrade/strip), `Send` **fails closed** with
`ErrSendFailed` rather than transmitting the message + recipient list in
cleartext. Set `SMTPConfig.AllowCleartext: true` to explicitly opt into
plaintext delivery (local relays / dev only).

**Templates:**
```go
// LoadFromDir takes a filesystem path; LoadFromFS takes an fs.FS
// (handy for go:embed). Either returns map[string]email.Template.
tmpls, _ := email.LoadFromDir("emails")
msg, _ := email.Execute(tmpls["welcome"], map[string]any{"Name": user.Name})
msg.To = []string{user.Email}
_ = sender.Send(ctx, msg)
```

**AI-typical anti-pattern** — if you're about to write any of these,
stop and use `Sender` / `Email` instead:
- `net/smtp.SendMail(addr, auth, from, to, msg)` with a hand-built
  `Subject:\nFrom:\nTo:\n\nbody` byte slice
- `mime/multipart` boilerplate to attach files
- A `func sendEmail(to, subject, body string)` helper that swallows
  the SMTP error
- A `_, _ = client.SendMail(...)` because "email is best-effort"
  (it isn't — failed sends matter; surface the error)

`email.Email{}` handles To/CC/BCC/HTML/Text/Attachments uniformly,
and `Sender` lets you swap LogSender (dev) for SMTPSender (prod)
without changing call sites.

**For PHI-bearing apps:** never put diagnostic content (symptom logs,
triggers, search history) in `Subject` — subjects show in inbox
preview panes. Body content is at the user's email-provider's
discretion to retain indefinitely.

**Composing with notify:** wrap the Sender in `notify.NewEmailChannel`
to drop into `battery/notify`'s fan-out instead of calling `Send`
directly from handlers.
