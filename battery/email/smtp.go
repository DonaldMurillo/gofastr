package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"maps"
	"net"
	"net/mail"
	"net/smtp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

const defaultSMTPDialTimeout = 10 * time.Second

// SMTPConfig holds the configuration for an SMTP sender.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	// UseTLS dials with implicit TLS (e.g. port 465).
	UseTLS bool
	// AllowCleartext, when true, permits the message to be transmitted
	// without any transport encryption (no implicit TLS and STARTTLS either
	// unavailable or not negotiated).
	AllowCleartext bool

	// DialTimeout bounds the TCP (and, for UseTLS, the TLS handshake)
	// connect.
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

func (c SMTPConfig) hostname() string {
	if strings.HasPrefix(c.Host, "[") && strings.HasSuffix(c.Host, "]") {
		return c.Host[1 : len(c.Host)-1]
	}
	return c.Host
}

func (c SMTPConfig) addr() string {
	return net.JoinHostPort(c.hostname(), strconv.Itoa(c.Port))
}

// Send implements the Sender interface using SMTP.
func (s *SMTPSender) Send(ctx context.Context, email Email) (sendErr error) {
	if err := s.config.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	if len(email.To) == 0 {
		return fmt.Errorf("%w: at least one recipient is required", ErrSendFailed)
	}

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

	msg, err := buildMessage(email)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}

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
	stopOnCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stopOnCancel()
		if sendErr != nil && ctx.Err() != nil {
			sendErr = fmt.Errorf("%w: %w", ErrSendFailed, ctx.Err())
		}
	}()
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	if s.config.UseTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: s.config.hostname()})
		if err := tlsConn.HandshakeContext(dialCtx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("%w: tls handshake failed: %v", ErrSendFailed, err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, s.config.hostname())
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("%w: smtp client creation failed: %v", ErrSendFailed, err)
	}
	defer client.Close()

	if !s.config.UseTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsConfig := &tls.Config{ServerName: s.config.hostname()}
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("%w: starttls failed: %v", ErrSendFailed, err)
			}
		} else if !s.config.AllowCleartext {
			return fmt.Errorf("%w: server does not advertise STARTTLS and AllowCleartext is false: refusing to send in cleartext (set SMTPConfig.AllowCleartext to override)", ErrSendFailed)
		}
	}

	if s.config.Username != "" && s.config.Password != "" {
		if ok, offered := client.Extension("AUTH"); ok {
			auth, err := s.pickAuth(offered)
			if err != nil {
				return err
			}
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("%w: auth failed: %v", ErrSendFailed, err)
			}
		} else {
			return fmt.Errorf("%w: server does not advertise AUTH", ErrSendFailed)
		}
	}

	if err := client.Mail(fromSpecs[0]); err != nil {
		return fmt.Errorf("%w: mail from failed: %v", ErrSendFailed, err)
	}

	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("%w: rcpt to failed: %v", ErrSendFailed, err)
		}
	}

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

	_ = client.Quit()
	return nil
}

func checkCustomHeaders(headers map[string]string) error {
	for k, v := range headers {
		if err := assertNoHeaderInjection(k, k); err != nil {
			return err
		}
		if err := assertNoHeaderInjection(k, v); err != nil {
			return err
		}
	}
	seen := make(map[string]string, len(headers))
	for _, k := range slices.Sorted(maps.Keys(headers)) {
		if !isFieldName(k) {
			return fmt.Errorf("%w: Headers key %q is not a header field name (printable ASCII, no colon or space)", ErrSendFailed, k)
		}
		folded := asciicase.Lower(k)
		if prev, dup := seen[folded]; dup {
			return fmt.Errorf("%w: Headers keys %q and %q name one field", ErrSendFailed, prev, k)
		}
		seen[folded] = k
	}
	for _, name := range []string{"Content-Type", "Content-Transfer-Encoding", "MIME-Version", "From", "To", "Cc", "Bcc", "Resent-Bcc", "Subject"} {
		if hasHeader(headers, name) {
			return fmt.Errorf("%w: Headers cannot set %s; the sender writes it from the Email", ErrSendFailed, name)
		}
	}
	return nil
}

func writeCustomHeaders(buf *strings.Builder, headers map[string]string) error {
	for _, k := range slices.Sorted(maps.Keys(headers)) {
		if err := writeHeader(buf, scrubHeaderValue(k), scrubHeaderValue(headers[k])); err != nil {
			return err
		}
	}
	return nil
}

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
		spec, err := envelopeSpec(a.Address)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %v", field, raw, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// ValidateHeaders applies the same custom header checks as [SMTPSender.Send].
// It returns an error wrapping [ErrSendFailed] for unsafe names or values,
// duplicate or reserved names, and lines that cannot be folded safely.
func ValidateHeaders(headers map[string]string) error {
	if err := checkCustomHeaders(headers); err != nil {
		return err
	}
	return writeCustomHeaders(nil, headers)
}

// ValidateAddress reports whether addr is one mailbox [SMTPSender] sends
// to or from: free of control bytes and invisible codepoints (the header
// check Send applies), parsed by net/mail as exactly one address, with a
// local part a relay reads as one path (the envelope check).
func ValidateAddress(addr string) error {
	if err := assertNoHeaderInjection("address", addr); err != nil {
		return err
	}
	specs, err := scrubEnvelopeAddrs("address", addr)
	if err != nil {
		return err
	}
	if len(specs) != 1 {
		return fmt.Errorf("address %q is %d mailboxes, want one", addr, len(specs))
	}
	return nil
}

func envelopeSpec(addr string) (string, error) {
	local := addr[:max(strings.LastIndexByte(addr, '@'), 0)]
	if strings.ContainsAny(local, "<>@") {
		return "", fmt.Errorf("local part %q holds <, > or @: refusing envelope address", local)
	}
	s := (&mail.Address{Address: addr}).String()
	return s[1 : len(s)-1], nil
}

func envelopeRecipients(email Email) ([]string, error) {
	recipients := make([]string, 0, len(email.To)+len(email.CC)+len(email.BCC))
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
