# Email

`battery/email` sends transactional email over SMTP and renders it from
templates. It is a plain library. There is no `framework.WithEmail`. You
construct a `Sender` and call `Send` from wherever a message needs to go
out: a hook, a queue worker, or a handler.

The package ships two senders behind one `Sender` interface:
`SMTPSender` for production and `LogSender` for development.

## Send

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

## The Email type

| Field         | Type             | Notes                                              |
|---------------|------------------|----------------------------------------------------|
| `From`        | `string`         | Required.                                          |
| `To`          | `[]string`       | Required. Recipients.                              |
| `CC`          | `[]string`       | Optional.                                          |
| `BCC`         | `[]string`       | Optional.                                          |
| `Subject`     | `string`         |                                                    |
| `TextBody`    | `string`         | Plain-text body.                                   |
| `HTMLBody`    | `string`         | Optional HTML body. When both bodies are set the   |
|               |                  | message is MIME multipart.                         |
| `Attachments` | `[]Attachment`   | Optional.                                          |
| `Headers`     | `map[string]string` | Optional custom headers.                       |

`Attachment` carries `Filename`, `Content` (`[]byte`), and `ContentType`.

## SMTP sender

`SMTPConfig`:

| Field            | Type             | Notes                                                          |
|------------------|------------------|----------------------------------------------------------------|
| `Host`           | `string`         | Required.                                                      |
| `Port`           | `int`            | Required.                                                      |
| `Username`       | `string`         | Optional (a cleartext relay may leave it empty).               |
| `Password`       | `string`         | Optional.                                                      |
| `UseTLS`         | `bool`           | Implicit TLS (e.g. port 465). False (default) attempts         |
|                  |                  | STARTTLS on the cleartext connection.                          |
| `AllowCleartext` | `bool`           | Send unencrypted when neither implicit TLS nor STARTTLS is     |
|                  |                  | available. Default false: `Send` fails closed instead of       |
|                  |                  | leaking the message and recipient list.                        |
| `DialTimeout`    | `time.Duration`  | TCP+TLS connect budget. Zero = 10s. The same budget is set as  |
|                  |                  | the connection's I/O deadline, so a host that accepts the dial |
|                  |                  | and then stalls cannot hang the worker.                        |

`SMTPConfig.Validate()` checks the required fields.

The sender refuses to serialize an `Email` whose header fields (`From`,
`To`, `Cc`, `Bcc`, `Subject`, custom headers, attachment filename and
content type) contain any C0 control byte (including CR, LF, NUL) or DEL.
Without that check, a value like `"foo\r\nBcc: victim@e.com"` would smuggle
an extra recipient onto the outgoing message, and other control bytes reach
MUAs and spam filters verbatim. MIME boundaries are cryptographically random
and checked against the body, so template output cannot inject or terminate
a MIME part.

The SMTP envelope is built separately from the headers: every `From` / `To`
/ `Cc` / `Bcc` entry is parsed with `net/mail` and reaches the wire as
exactly one bare addr-spec (`MAIL FROM:<addr-spec>`, one `RCPT TO` per
recipient). A display-name form like `"Bob <bob@x>"` or a comma-joined
entry is expanded the same way the header side reads it, so the envelope
recipient set can never diverge from the parsed header set; entries that
fail to parse, parse to zero addresses, or carry control bytes are refused.
`Send` also never writes into the caller's slices — the recipient list is
an explicit-length copy, so a reused `To` backing array cannot observe a
BCC address.

## Log sender (development)

<!-- gofastr:compile
stmt: _ = sender
import "github.com/DonaldMurillo/gofastr/battery/email"
-->
```go
sender := email.NewLogSender() // writes to stdout
```

`LogSender` writes the rendered message to an `io.Writer` instead of
sending it. Before logging, it redacts URL query strings carrying
`token` / `code` / `key` / `secret` / `password` and any
`Bearer <token>` substring. Password-reset and magic-link emails render
with their secrets stripped, so a dev log never captures a live
credential.

## Templates

`Template` holds a `Subject`, `TextBody`, and `HTMLBody`, any of which
may contain Go template directives (`{{.Name}}`). `Execute` renders all
three against a `map[string]any` and returns an `Email`:

```go
tmpls, err := email.LoadFromDir("templates/email")
// templates/email/welcome.txt + welcome.html → tmpls["welcome"]

msg, err := email.Execute(tmpls["welcome"], map[string]any{"Name": "Carol"})
msg.From = "no-reply@example.com"
msg.To = []string{user.Email}
sender.Send(ctx, msg)
```

`Execute` renders the subject and text body with `text/template` and the
HTML body with `html/template`, so HTML escaping applies only to the HTML
part. `From` and `To` are the caller's job. They vary per recipient,
not per template.

`LoadFromDir` pairs `welcome.txt` + `welcome.html` into one template
named `welcome`; the subject comes from a leading `Subject: ` line in
the `.txt` file, or a sibling `.subject` file. `LoadFromFS` does the
same against an `fs.FS`, so an embedded template tree works.

## Common mistakes

- **Leaving `AllowCleartext` off and pointing at a host that strips STARTTLS.** That is the intended failure: the message does not go out in plaintext. Fix the relay (use a TLS-capable host, or port 465 with `UseTLS`), not the flag.
- **Letting recipient input reach `Email` unvalidated.** The sender rejects CR/LF/NUL in headers and fails loudly rather than smuggling a `Bcc`, but a malformed address still wastes a send attempt. Validate addresses before they reach `Email.To`.
- **Using `LogSender` in production by accident.** It never sends; email looks fine in dev and silently does nothing in prod. Build the sender from an env flag instead of hardcoding `NewLogSender()`.
- **Forgetting `From` / `To` after `Execute`.** `Execute` fills the subject and bodies only; an `Email` with no recipients is a no-op `Send`.
