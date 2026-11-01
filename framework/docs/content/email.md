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
|               |                  | message is `multipart/alternative` (text first,    |
|               |                  | HTML last), so a client shows one of them.         |
| `Attachments` | `[]Attachment`   | Optional.                                          |
| `Headers`     | `map[string]string` | Optional custom headers. `Content-Type`,       |
|               |                  | `Content-Transfer-Encoding` and `MIME-Version`   |
|               |                  | are refused: the sender writes them.             |

`Attachment` carries `Filename`, `Content` (`[]byte`), and `ContentType`.
A message with attachments is `multipart/mixed`: the body comes first
(the `multipart/alternative` part, or the single text or HTML part), then
one base64 part per attachment.

## SMTP sender

`SMTPConfig`:

| Field            | Type             | Notes                                                          |
|------------------|------------------|----------------------------------------------------------------|
| `Host`           | `string`         | Required.                                                      |
| `Port`           | `int`            | Required.                                                      |
| `Username`       | `string`         | Optional (a cleartext relay may leave it empty).               |
| `Password`       | `string`         | Optional. With both set, `Send` logs in with one mechanism the |
|                  |                  | relay advertises: CRAM-MD5, else PLAIN, else LOGIN. PLAIN and  |
|                  |                  | LOGIN run only over TLS or to localhost. A relay offering none |
|                  |                  | of the three fails with an error naming what it offers.        |
| `UseTLS`         | `bool`           | Implicit TLS (e.g. port 465). False (default) attempts         |
|                  |                  | STARTTLS on the cleartext connection.                          |
| `AllowCleartext` | `bool`           | Send unencrypted when neither implicit TLS nor STARTTLS is     |
|                  |                  | available. Default false: `Send` fails closed instead of       |
|                  |                  | leaking the message and recipient list.                        |
| `DialTimeout`    | `time.Duration`  | TCP+TLS connect budget. Zero = 10s. The same budget is set as  |
|                  |                  | the connection's I/O deadline, so a host that accepts the dial |
|                  |                  | and then stalls cannot hang the worker.                        |

`SMTPConfig.Validate()` checks the required fields.

`Send` honours its context for the whole exchange, not only the dial:
cancelling it closes the connection, and the send returns an error
wrapping both `ErrSendFailed` and the context's error
(`context.Canceled` or `context.DeadlineExceeded`). Once the relay has
accepted the message (its reply to the end of `DATA`), a later cancel
does not turn the send into a failure.

The sender refuses to serialize an `Email` whose header fields (`From`,
`To`, `Cc`, `Bcc`, `Subject`, custom headers, attachment filename and
content type) contain any C0 control byte (including CR, LF, NUL) or DEL.
Without that check, a value like `"foo\r\nBcc: victim@e.com"` would smuggle
an extra recipient onto the outgoing message, and other control bytes reach
MUAs and spam filters verbatim. MIME boundaries (one per container, the
nested alternative included) are cryptographically random and checked
against the body, so template output cannot inject or terminate a MIME
part.

Header text that is not ASCII goes out encoded, so a relay that does not
offer SMTPUTF8 accepts it: a `Subject` such as `¡Confirma tu correo!`
and a display name in `From`, `To` or `Cc` (`José Pérez <jose@example.com>`)
are written as RFC 2047 encoded words, and an attachment filename as an
RFC 2231 `name*=` / `filename*=` parameter. ASCII values keep their
bytes. A mailbox address that is not ASCII (`josé@example.com`) cannot be
encoded and is sent as written; it needs a relay that offers SMTPUTF8,
which `Send` requests when the relay advertises it. Custom `Headers`
values are written as given.

`email.ValidateAddress(addr)` returns nil when `Send` takes `addr` as one
mailbox (a bare address or `Name <addr>`, any local part RFC 5322 allows,
an apostrophe included) and an error for a control byte, an invisible
codepoint, an unparsable value or a comma list. Code that hands addresses
to a `Sender` checks them with it rather than with a rule of its own.

A long header is folded (RFC 5322) so no line passes 998 octets: the
line breaks in front of a space and a reader joins it back unchanged. A
`Subject` word too long for one line goes out as encoded words, split
between characters, and a long attachment filename as RFC 2231
continuations (`filename*0*=`, `filename*1*=`, ...). `Send` refuses a
custom header or address with a single word over 998 octets, which no
fold can carry.

Both bodies go out quoted-printable (RFC 2045): CR, LF and CRLF line
endings all become CRLF, no line passes 76 octets, and non-ASCII text is
escaped, so a template saved with Unix line endings or a minified
one-line HTML body reaches the recipient intact. The boundary check reads
the encoded body, and refuses any line that starts with a delimiter.

Every message carries a `Date` and a `Message-ID` of the form
`<32 hex digits@domain>`, random per message. The domain is the
first `From` address's when it is a plain host name, and `localhost`
otherwise. A `Date` or `Message-ID` key in `Headers` (any ASCII
case) replaces the stamped one, so a message never carries two.

`Send` succeeds once the relay answers `250` to the message: the mail is
queued. A failed `QUIT` after that (the relay drops the connection or
stalls) is ignored, so a retrying queue does not deliver the message
twice. Every failure before acceptance wraps `ErrSendFailed`.

The SMTP envelope is built separately from the headers: every `From` / `To`
/ `Cc` / `Bcc` entry is parsed with `net/mail` and reaches the wire as
exactly one bare addr-spec (`MAIL FROM:<addr-spec>`, one `RCPT TO` per
recipient). A display-name form like `"Bob <bob@x>"` or a comma-joined
entry is expanded the same way the header side reads it, so the envelope
recipient set can never diverge from the parsed header set; entries that
fail to parse, parse to zero addresses, or carry control bytes are refused.
A local part that needs quoting keeps its quotes on the wire
(`"john doe"@example.com`), and one holding `<`, `>` or `@` is refused,
quoted or not: a relay that ends the path at the first `>` would read a
different recipient out of it.
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
credential. The redaction is exported as `email.RedactBody`, so sibling
dev-log surfaces (notify's `LoggerChannel`) scrub the same shapes
instead of forging their own patterns. Every printed value is scrubbed of control, C1 and bidi characters
(bodies keep their newlines and tabs), so an escape sequence in a
template or an attachment name never reaches the terminal.

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
the `.txt` file, or a sibling `.subject` file. Files saved with CRLF line
endings work: the `\r` is trimmed off the subject line. `LoadFromFS` does
the same against an `fs.FS`, so an embedded template tree works.

To read a template before it sends, list it in the admin's
`Config.EmailPreviews`: the Emails page renders each one with sample
data, the HTML body in a sandboxed frame (see
[admin → Email previews](admin.md#email-previews)).

## Common mistakes

- **Leaving `AllowCleartext` off and pointing at a host that strips STARTTLS.** That is the intended failure: the message does not go out in plaintext. Fix the relay (use a TLS-capable host, or port 465 with `UseTLS`), not the flag.
- **Letting recipient input reach `Email` unvalidated.** The sender rejects CR/LF/NUL in headers and fails loudly rather than smuggling a `Bcc`, but a malformed address still wastes a send attempt. Validate addresses before they reach `Email.To`.
- **Using `LogSender` in production by accident.** It never sends; email looks fine in dev and silently does nothing in prod. Build the sender from an env flag instead of hardcoding `NewLogSender()`.
- **Forgetting `From` / `To` after `Execute`.** `Execute` fills the subject and bodies only; an `Email` with no recipients is a no-op `Send`.
