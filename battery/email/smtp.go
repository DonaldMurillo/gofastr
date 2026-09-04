package email

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"maps"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

// defaultSMTPDialTimeout bounds the TCP+TLS connect, and, as the
// connection's I/O deadline, the whole SMTP exchange, when SMTPConfig.
// DialTimeout is unset. A black-holed or stalling SMTP host would
// otherwise block the calling worker forever (the DBQueue's single
// default worker especially).
const defaultSMTPDialTimeout = 10 * time.Second

// SMTPConfig holds the configuration for an SMTP sender.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	// UseTLS dials with implicit TLS (e.g. port 465). When false, the
	// sender opportunistically attempts STARTTLS on the cleartext
	// connection.
	UseTLS bool
	// AllowCleartext, when true, permits the message to be transmitted
	// without any transport encryption (no implicit TLS and STARTTLS
	// either unavailable or not negotiated). It defaults to false so the
	// sender fails CLOSED: if neither implicit TLS nor STARTTLS could be
	// established, Send returns an error instead of leaking the message
	// and recipient list in cleartext (defends against STARTTLS
	// stripping by an on-path attacker). Set true only for trusted
	// local relays where plaintext is acceptable.
	AllowCleartext bool

	// DialTimeout bounds the TCP (and, for UseTLS, the TLS handshake)
	// connect. Zero uses defaultSMTPDialTimeout (10s). The request
	// context's deadline still applies and wins when it is sooner.
	// The same budget is also set as the connection's I/O deadline, so
	// a server that accepts the dial and then stalls mid-exchange
	// (never sends the greeting, wedges after MAIL) cannot hang the
	// worker either.
	DialTimeout time.Duration
}

// SMTPSender sends emails via SMTP.
type SMTPSender struct {
	config SMTPConfig
}

// NewSMTPSender creates a new SMTPSender with the given configuration.
func NewSMTPSender(config SMTPConfig) *SMTPSender {
	return &SMTPSender{config: config}
}

// Validate checks that the SMTPConfig has required fields.
func (c SMTPConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("email: smtp host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("email: smtp port must be between 1 and 65535")
	}
	return nil
}

// addr returns the host:port address string.
func (c SMTPConfig) addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Send implements the Sender interface using SMTP.
func (s *SMTPSender) Send(ctx context.Context, email Email) error {
	if err := s.config.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	if len(email.To) == 0 {
		return fmt.Errorf("%w: at least one recipient is required", ErrSendFailed)
	}

	// Envelope, not headers. The message HEADERS keep the raw fields
	// (display names and comma lists are legal header syntax); the
	// ENVELOPE is normalized separately because net/smtp writes its
	// argument as MAIL FROM:<%s> with no quoting of its own: a
	// display-name or comma-joined entry emitted verbatim makes the
	// envelope recipient set diverge from the parsed header set
	// (strict MTAs 501 the whole send; a lenient relay that splits on
	// the comma delivers to an address the application never
	// confirmed). Every entry is parsed with net/mail and reaches the
	// wire as exactly one bare addr-spec; entries that fail to parse,
	// parse to zero addresses, or carry C0/DEL bytes are refused —
	// fail closed.
	fromSpecs, ferr := scrubEnvelopeAddrs("From", email.From)
	if ferr != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, ferr)
	}
	if len(fromSpecs) != 1 {
		return fmt.Errorf("%w: From must be exactly one address, got %d", ErrSendFailed, len(fromSpecs))
	}
	recipients, rerr := envelopeRecipients(email)
	if rerr != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, rerr)
	}

	addr := s.config.addr()

	// Build the email message.
	msg, err := buildMessage(email)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}

	// Connect to the server with a bounded dial. smtp.Dial / tls.Dial
	// ignore ctx and never time out on their own, a black-holed host
	// would otherwise wedge the worker forever.
	timeout := s.config.DialTimeout
	if timeout <= 0 {
		timeout = defaultSMTPDialTimeout
	}
	dialCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	dialer := &net.Dialer{Timeout: timeout}

	conn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("%w: smtp dial failed: %v", ErrSendFailed, err)
	}
	// The deadline covers the WHOLE SMTP exchange, not only the dial:
	// net/smtp has no timeouts of its own, so a server that accepts the
	// connection and then stalls (never sends the 220 greeting, wedges
	// mid-exchange) would otherwise hang the worker forever.
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	if s.config.UseTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: s.config.Host})
		if err := tlsConn.HandshakeContext(dialCtx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("%w: tls handshake failed: %v", ErrSendFailed, err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, s.config.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: smtp client creation failed: %v", ErrSendFailed, err)
	}
	defer client.Close()

	// Attempt STARTTLS if not already using implicit TLS. Fail CLOSED:
	// if the server does not advertise STARTTLS (e.g. an on-path
	// attacker stripped the capability) or negotiation fails, refuse to
	// continue over cleartext unless AllowCleartext was explicitly set.
	if !s.config.UseTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{ServerName: s.config.Host}
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("%w: starttls failed: %v", ErrSendFailed, err)
			}
		} else if !s.config.AllowCleartext {
			return fmt.Errorf("%w: server does not advertise STARTTLS and AllowCleartext is false: refusing to send in cleartext (set SMTPConfig.AllowCleartext to override)", ErrSendFailed)
		}
	}

	// Authenticate if credentials are provided.
	if s.config.Username != "" && s.config.Password != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			// Try CRAM-MD5 first, then fall back to PLAIN.
			auth := smtp.CRAMMD5Auth(s.config.Username, s.config.Password)
			if err := client.Auth(auth); err != nil {
				// Fall back to PLAIN auth.
				auth = smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)
				if err := client.Auth(auth); err != nil {
					return fmt.Errorf("%w: auth failed: %v", ErrSendFailed, err)
				}
			}
		}
	}

	// Set the sender. The envelope carries the parsed bare addr-spec,
	// never the raw From string.
	if err := client.Mail(fromSpecs[0]); err != nil {
		return fmt.Errorf("%w: mail from failed: %v", ErrSendFailed, err)
	}

	// Add recipients.
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("%w: rcpt to failed: %v", ErrSendFailed, err)
		}
	}

	// Send the email body.
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("%w: data failed: %v", ErrSendFailed, err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("%w: write failed: %v", ErrSendFailed, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("%w: close failed: %v", ErrSendFailed, err)
	}

	return client.Quit()
}

// buildMessage constructs the raw email message bytes. It refuses
// to serialise an Email whose header fields contain CR or LF,
// without that check, a To/From/Subject/custom-header value of
// `"foo\r\nBcc: victim@e.com"` would smuggle an extra Bcc onto the
// outgoing message (classic SMTP header injection).
func buildMessage(email Email) ([]byte, error) {
	if err := assertNoHeaderInjection("From", email.From); err != nil {
		return nil, err
	}
	if err := assertNoHeaderInjection("Subject", email.Subject); err != nil {
		return nil, err
	}
	for _, a := range email.To {
		if err := assertNoHeaderInjection("To", a); err != nil {
			return nil, err
		}
	}
	for _, a := range email.CC {
		if err := assertNoHeaderInjection("Cc", a); err != nil {
			return nil, err
		}
	}
	for _, a := range email.BCC {
		if err := assertNoHeaderInjection("Bcc", a); err != nil {
			return nil, err
		}
	}
	for k, v := range email.Headers {
		if err := assertNoHeaderInjection(k, k); err != nil {
			return nil, err
		}
		if err := assertNoHeaderInjection(k, v); err != nil {
			return nil, err
		}
	}
	for _, att := range email.Attachments {
		if err := assertNoHeaderInjection("Attachment.Filename", att.Filename); err != nil {
			return nil, err
		}
		if err := assertNoHeaderInjection("Attachment.ContentType", att.ContentType); err != nil {
			return nil, err
		}
	}

	var buf strings.Builder

	// Headers. Every caller-derived value passes scrubHeaderValue so
	// no C0/DEL byte can reach a header line even if a future code
	// path skips assertNoHeaderInjection (defense in depth; the assert
	// is the fail-closed gate, the scrub is the wire guarantee).
	buf.WriteString("From: " + scrubHeaderValue(email.From) + "\r\n")
	buf.WriteString("To: " + scrubHeaderValue(strings.Join(email.To, ", ")) + "\r\n")
	if len(email.CC) > 0 {
		buf.WriteString("Cc: " + scrubHeaderValue(strings.Join(email.CC, ", ")) + "\r\n")
	}
	buf.WriteString("Subject: " + scrubHeaderValue(email.Subject) + "\r\n")

	// Custom headers, sorted so the wire message is byte-stable per send.
	for _, k := range slices.Sorted(maps.Keys(email.Headers)) {
		buf.WriteString(scrubHeaderValue(k) + ": " + scrubHeaderValue(email.Headers[k]) + "\r\n")
	}

	// Determine if we need MIME multipart.
	hasAttachments := len(email.Attachments) > 0
	hasTextAndHTML := email.TextBody != "" && email.HTMLBody != ""

	if hasAttachments || hasTextAndHTML {
		// Use a cryptographically random boundary per message so body
		// content (which may be rendered from attacker-influenced
		// templates) cannot predict and forge the delimiter to inject a
		// new MIME part or terminate the container early.
		boundary, err := randomBoundary()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSendFailed, err)
		}
		// Defense in depth: even with an unguessable boundary, refuse to
		// serialise a body that contains a line forming the delimiter.
		if bodyContainsBoundary(email.TextBody, boundary) || bodyContainsBoundary(email.HTMLBody, boundary) {
			return nil, fmt.Errorf("%w: message body contains the MIME boundary delimiter (refusing to send to prevent MIME part injection)", ErrSendFailed)
		}
		buf.WriteString("MIME-Version: 1.0\r\n")
		buf.WriteString("Content-Type: multipart/mixed; boundary=" + boundary + "\r\n")
		buf.WriteString("\r\n")

		// Text body part
		if email.TextBody != "" {
			buf.WriteString("--" + boundary + "\r\n")
			buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
			buf.WriteString("\r\n")
			buf.WriteString(email.TextBody + "\r\n")
		}

		// HTML body part
		if email.HTMLBody != "" {
			buf.WriteString("--" + boundary + "\r\n")
			buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
			buf.WriteString("\r\n")
			buf.WriteString(email.HTMLBody + "\r\n")
		}

		// Attachments
		for _, att := range email.Attachments {
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			// Encode the Content-Type/Disposition parameters with
			// mime.FormatMediaType, which quotes/escapes embedded
			// double-quotes and special characters (RFC 2045/2231). Raw
			// concatenation would let a filename like `x"; name="evil`
			// break out of the quoted parameter and append attacker
			// parameters.
			// Validate the content-type as a media type so a forged value
			// like `text/csv"; name="evil` cannot reach the header. The
			// base type (before any params) is re-emitted, and the
			// filename is emitted as a properly escaped quoted-string.
			baseCT, err := safeMediaType(ct)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid attachment content-type: %v", ErrSendFailed, err)
			}
			name := quoteParamValue(att.Filename)
			buf.WriteString("--" + boundary + "\r\n")
			buf.WriteString("Content-Type: " + scrubHeaderValue(baseCT) + "; name=" + name + "\r\n")
			buf.WriteString("Content-Transfer-Encoding: base64\r\n")
			buf.WriteString("Content-Disposition: attachment; filename=" + name + "\r\n")
			buf.WriteString("\r\n")
			buf.WriteString(encodeBase64(att.Content))
			buf.WriteString("\r\n")
		}

		buf.WriteString("--" + boundary + "--\r\n")
	} else {
		// Simple single-part message.
		if email.HTMLBody != "" {
			buf.WriteString("MIME-Version: 1.0\r\n")
			buf.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		} else {
			buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		}
		buf.WriteString("\r\n")
		if email.TextBody != "" {
			buf.WriteString(email.TextBody)
		} else if email.HTMLBody != "" {
			buf.WriteString(email.HTMLBody)
		}
	}

	return []byte(buf.String()), nil
}

// encodeBase64 wraps base64-encoded content at 76 characters per line.
func encodeBase64(data []byte) string {
	return b64Encode(data)
}

// randomBoundary returns a cryptographically random MIME boundary
// token. The token uses only RFC 2046 boundary characters (hex), so it
// never needs quoting and cannot be predicted by an attacker crafting
// body content.
func randomBoundary() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("boundary generation failed: %w", err)
	}
	return "gofastr-boundary-" + hex.EncodeToString(b[:]), nil
}

// bodyContainsBoundary reports whether any line of body equals the
// boundary delimiter (`--boundary`) or the closing delimiter
// (`--boundary--`), which would let the body inject or terminate a MIME
// part. Lines are split on both CRLF and bare LF since either could be
// present in template-rendered content.
func bodyContainsBoundary(body, boundary string) bool {
	delim := "--" + boundary
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == delim || line == delim+"--" {
			return true
		}
	}
	return false
}

// safeMediaType parses ct and returns only its canonical base media
// type (e.g. "text/csv"), discarding any caller-supplied parameters. A
// forged value such as `text/csv"; name="evil` fails to parse as a
// bare media type, so we fail closed rather than emit it into a header.
func safeMediaType(ct string) (string, error) {
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return "", fmt.Errorf("cannot parse media type %q: %w", ct, err)
	}
	return mt, nil
}

// quoteParamValue renders s as an RFC 2045 quoted-string, backslash-
// escaping embedded double-quotes and backslashes so the value cannot
// break out of the surrounding `"..."` and append extra MIME
// parameters. The full C0 range and DEL are stripped: control bytes
// are never legitimate in a display filename, and percent-encoding
// them would surface literal %xx garbage in every MUA. C0/DEL are
// already refused upstream by assertNoHeaderInjection; the strip here
// is the defensive wire-side guarantee.
func quoteParamValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	b.WriteByte('"')
	for i := range len(s) {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// scrubHeaderValue percent-encodes any C0 control byte or DEL in a
// header value before it is written into the message, so a value that
// slips past assertNoHeaderInjection still cannot terminate a header
// line or smuggle a terminal-control payload into a MUA. Same
// double posture as quoteParamValue: the assert refuses, the scrub
// guarantees the wire.
func scrubHeaderValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			fmt.Fprintf(&b, "%%%02x", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// assertNoHeaderInjection returns an error if value contains any C0
// control byte (0x00–0x1F, including the CR/LF/NUL that can terminate
// a header line and let following bytes appear as a new header) or DEL.
// The rest of the C0 range and DEL are refused too, not just the line
// terminators: MUAs, spam filters and archive tooling render header
// bytes verbatim, so an ESC…BEL terminal-title sequence in a Subject
// is smuggled onto the wire exactly like a CRLF injection. The field
// name is included in the error so the caller can log which input was
// rejected.
func assertNoHeaderInjection(field, value string) error {
	for i := range len(value) {
		if b := value[i]; b < 0x20 || b == 0x7f {
			return fmt.Errorf("%w: header %q contains illegal control byte 0x%02X (C0/DEL): refusing to send to prevent SMTP header injection",
				ErrSendFailed, field, b)
		}
	}
	return nil
}

// scrubEnvelopeAddrs normalises one envelope address entry for the
// wire: the raw value must carry no C0 control byte or DEL (refused
// outright, not encoded — an encoded address would never route) and
// must parse under net/mail; the bare addr-spec of every address the
// entry expands to is returned. A comma-joined entry expands exactly
// the way the header side (net/mail) reads it, so the envelope
// recipient set can never diverge from the parsed header set. Entries
// that fail to parse or parse to zero addresses are refused — the
// guard cannot decide, so it refuses.
func scrubEnvelopeAddrs(field, raw string) ([]string, error) {
	for i := range len(raw) {
		if b := raw[i]; b < 0x20 || b == 0x7f {
			return nil, fmt.Errorf("%s contains illegal control byte 0x%02X: refusing envelope address", field, b)
		}
	}
	addrs, err := mail.ParseAddressList(raw)
	if err != nil {
		return nil, fmt.Errorf("%s %q is not a valid address: %v", field, raw, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%s %q parses to zero addresses", field, raw)
	}
	specs := make([]string, 0, len(addrs))
	for _, a := range addrs {
		specs = append(specs, a.Address)
	}
	return specs, nil
}

// envelopeRecipients builds the RCPT TO list from To + CC + BCC. The
// list is an explicit-length copy: appending straight onto email.To
// (append(append(email.To, CC...), BCC...)) would write the CC/BCC
// strings into To's spare capacity, and any longer-lived alias over
// that backing array — a mail-merge buffer, a sibling slice — would
// observe the BCC address as a To entry.
func envelopeRecipients(email Email) ([]string, error) {
	recipients := make([]string, 0, len(email.To)+len(email.CC)+len(email.BCC))
	// Deterministic To → Cc → Bcc order (never range a map: the RCPT
	// sequence is wire output).
	fields := []struct {
		name string
		list []string
	}{
		{"To", email.To},
		{"Cc", email.CC},
		{"Bcc", email.BCC},
	}
	for _, f := range fields {
		for _, a := range f.list {
			specs, err := scrubEnvelopeAddrs(f.name, a)
			if err != nil {
				return nil, err
			}
			recipients = append(recipients, specs...)
		}
	}
	return recipients, nil
}
