package email

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// defaultSMTPDialTimeout bounds the TCP+TLS connect — and, as the
// connection's I/O deadline, the whole SMTP exchange — when SMTPConfig.
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

	addr := s.config.addr()

	// Build the email message.
	msg, err := buildMessage(email)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}

	// Collect all recipients (To + CC + BCC).
	recipients := append(append(email.To, email.CC...), email.BCC...)

	// Connect to the server with a bounded dial. smtp.Dial / tls.Dial
	// ignore ctx and never time out on their own — a black-holed host
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
	// The deadline covers the WHOLE SMTP exchange, not just the dial:
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
			return fmt.Errorf("%w: server does not advertise STARTTLS and AllowCleartext is false — refusing to send in cleartext (set SMTPConfig.AllowCleartext to override)", ErrSendFailed)
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

	// Set the sender.
	if err := client.Mail(email.From); err != nil {
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
// to serialise an Email whose header fields contain CR or LF —
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

	// Headers
	buf.WriteString("From: " + email.From + "\r\n")
	buf.WriteString("To: " + strings.Join(email.To, ", ") + "\r\n")
	if len(email.CC) > 0 {
		buf.WriteString("Cc: " + strings.Join(email.CC, ", ") + "\r\n")
	}
	buf.WriteString("Subject: " + email.Subject + "\r\n")

	// Custom headers
	for k, v := range email.Headers {
		buf.WriteString(k + ": " + v + "\r\n")
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
			buf.WriteString("Content-Type: " + baseCT + "; name=" + name + "\r\n")
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
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
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
// parameters. CR/LF/NUL are already rejected upstream by
// assertNoHeaderInjection; they are stripped here defensively.
func quoteParamValue(s string) string {
	s = strings.NewReplacer("\r", "", "\n", "", "\x00", "").Replace(s)
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// assertNoHeaderInjection returns an error if value contains CR, LF, or
// NUL — the only bytes that can terminate a header line in SMTP's
// "Field: value\r\n" framing and let following bytes appear as a new
// header. The field name is included in the error so the caller can
// log which input was rejected.
func assertNoHeaderInjection(field, value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%w: header %q contains illegal control character (CR/LF/NUL — refusing to send to prevent SMTP header injection)",
			ErrSendFailed, field)
	}
	return nil
}
