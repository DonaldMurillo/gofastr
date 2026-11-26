# Email (`battery/email`)

`battery/email` sends transactional email over SMTP and renders Go templates. It is a plain Go library with no app-level option — construct a `Sender` and call `Send` from a hook, handler, or background queue worker.

Two senders share the `Sender` interface: `SMTPSender` for production and `LogSender` for local development.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/email"

sender := email.NewSMTPSender(email.SMTPConfig{
	Host:     "smtp.example.com",
	Port:     587,
	Username: "postmaster@example.com",
	Password: os.Getenv("SMTP_PASSWORD"),
})

err := sender.Send(ctx, email.Email{
	From:     "no-reply@example.com",
	To:       []string{"user@example.com"},
	Subject:  "Welcome",
	TextBody: "Thanks for signing up.",
})
```

For development (writes to stdout with `token`/`code`/`key`/`secret`/`password` query params and `Bearer` headers redacted via `email.RedactBody`):

<!-- gofastr:compile
stmt: _ = sender
import "github.com/DonaldMurillo/gofastr/battery/email"
-->
```go
sender := email.NewLogSender()
```

## Security and SMTP defaults

- **TLS required by default:** `UseTLS: true` uses implicit TLS (port 465); `false` upgrades via `STARTTLS`. `AllowCleartext` defaults to `false`, so sending fails closed rather than transmitting mail in plaintext.
- **Header-injection & MIME safety:** refuses C0 control bytes (CR, LF, NUL) and DEL in all address, subject, custom header, and attachment metadata fields; rejects reserved header overrides (`From`, `To`, `Cc`, `Bcc`, `Subject`, `Content-Type`, `MIME-Version`) in `Headers`; parses envelope addresses strictly (`email.ValidateAddress`) and uses cryptographically random MIME boundaries.
- **Encoding & context:** automatically handles RFC 2047 non-ASCII subjects/display names, RFC 2231 attachment filenames, quoted-printable bodies, `Date`/`Message-ID` headers, `DialTimeout` (default 10s), and context cancellation (closing the connection before `DATA` acceptance; ignoring post-`250` `QUIT` failures so retries don't double-send).
- **Multipart & attachments:** setting both `TextBody` and `HTMLBody` sends `multipart/alternative`; adding `Attachments` wraps the message in `multipart/mixed`.

## Templates

Load paired `.txt` / `.html` (plus `Subject: ` line or `.subject` file) templates from disk or an embedded `fs.FS`:

```go
tmpls, err := email.LoadFromDir("templates/email") // or email.LoadFromFS(...)
msg, err := email.Execute(tmpls["welcome"], map[string]any{"Name": "Carol"})
msg.From = "no-reply@example.com"
msg.To = []string{user.Email}
err = sender.Send(ctx, msg)
```

Both loaders treat a leading `Subject: ` line in either body file as metadata and remove it from that body. The first non-empty subject wins: the trimmed `.subject` file, then the `.txt` subject line, then the `.html` subject line. A body without a leading subject line is left unchanged by the loader.

`Execute` renders `Subject` and `TextBody` with `text/template` and `HTMLBody` with `html/template`. Preview templates with sample data inside `battery/admin` via `Config.EmailPreviews` (see [Admin email previews](admin.md#email-previews)).

## Common mistakes

- **Flipping `AllowCleartext: true` when a relay lacks STARTTLS:** use a TLS-capable relay or port 465 (`UseTLS: true`) instead of sending plaintext credentials and messages.
- **Shipping `NewLogSender()` in production:** select `SMTPSender` vs `LogSender` from environment configuration.
- **Forgetting `From` and `To` after `email.Execute`:** templates only fill subject and bodies.
- **Passing unvalidated user strings into `Email.To`:** validate addresses up front with `email.ValidateAddress`.
