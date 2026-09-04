// Package smtpsink is battery/email/smtp.go reduced: the message
// builder with assertNoHeaderInjection (CR/LF/NUL only) and
// quoteParamValue (quote-named, no control-range body evidence), plus
// the envelope calls. The pre-fix spellings are reported; the scrubbed
// spellings beside them are not. Reduced from the round-2 email probes
// TestEmailRedStripsHeaderControlBytes and
// TestEmailRedStripsParamControlBytes.
package smtpsink

import (
	"fmt"
	"net/smtp"
	"strings"
)

// Email mirrors battery/email's input struct: the package cannot see
// who built it, so at the wire its fields are untrusted.
type Email struct {
	From        string
	To          []string
	CC          []string
	Subject     string
	Headers     map[string]string
	TextBody    string
	Attachments []Attachment
}

type Attachment struct {
	Filename string
	Content  []byte
}

// send is SMTPSender.Send reduced to the envelope calls.
func send(client *smtp.Client, email Email) error {
	recipients := append(append(email.To, email.CC...), email.To...)
	if err := client.Mail(email.From); err != nil { // want `controlbytes: request-derived value reaches smtp.Client.Mail/Rcpt unscrubbed`
		return err
	}
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil { // want `controlbytes: request-derived value reaches smtp.Client.Mail/Rcpt unscrubbed`
			return err
		}
	}
	return nil
}

// sendScrubbed caps the envelope with the byte filter the fix ships.
func sendScrubbed(client *smtp.Client, email Email) error {
	if err := client.Mail(scrubHeader(email.From)); err != nil {
		return err
	}
	for _, rcpt := range email.To {
		if err := client.Rcpt(scrubHeader(rcpt)); err != nil {
			return err
		}
	}
	return nil
}

// buildMessage is the pre-fix builder: assertNoHeaderInjection rejects
// CR/LF/NUL only, so every other C0 byte and DEL pass into the header
// lines, and quoteParamValue strips the same narrow set.
func buildMessage(email Email) []byte {
	_ = assertNoHeaderInjection("From", email.From)
	_ = assertNoHeaderInjection("Subject", email.Subject)
	for _, a := range email.To {
		_ = assertNoHeaderInjection("To", a)
	}
	var buf strings.Builder
	buf.WriteString("From: " + email.From + "\r\n")                 // want `controlbytes: request-derived value reaches message header-line write unscrubbed`
	buf.WriteString("To: " + strings.Join(email.To, ", ") + "\r\n") // want `controlbytes: request-derived value reaches message header-line write unscrubbed`
	buf.WriteString("Cc: " + strings.Join(email.CC, ", ") + "\r\n") // want `controlbytes: request-derived value reaches message header-line write unscrubbed`
	buf.WriteString("Subject: " + email.Subject + "\r\n")           // want `controlbytes: request-derived value reaches message header-line write unscrubbed`
	for k, v := range email.Headers {
		_ = assertNoHeaderInjection(k, v)
		buf.WriteString(k + ": " + v + "\r\n") // want `controlbytes: request-derived value reaches message header-line write unscrubbed`
	}
	buf.WriteString("MIME-Version: 1.0\r\n")
	boundary := "boundary"
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString(email.TextBody + "\r\n")
	name := quoteParamValue(email.Attachments[0].Filename)
	buf.WriteString("Content-Type: application/octet-stream; name=" + name + "\r\n") // want `controlbytes: request-derived value reaches message header-line write unscrubbed`
	buf.WriteString("Content-Disposition: attachment; filename=" + name + "\r\n")    // want `controlbytes: request-derived value reaches message header-line write unscrubbed`
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	return []byte(buf.String())
}

// buildMessageScrubbed is the fixed spelling: every header value passes
// the byte filter at the sink.
func buildMessageScrubbed(email Email) []byte {
	var buf strings.Builder
	buf.WriteString("From: " + scrubHeader(email.From) + "\r\n")
	buf.WriteString("To: " + scrubHeader(strings.Join(email.To, ", ")) + "\r\n")
	buf.WriteString("Subject: " + scrubHeader(email.Subject) + "\r\n")
	for k, v := range email.Headers {
		buf.WriteString(scrubHeader(k) + ": " + scrubHeader(v) + "\r\n")
	}
	buf.WriteString("MIME-Version: 1.0\r\n")
	name := scrubHeader(email.Attachments[0].Filename)
	buf.WriteString("Content-Type: application/octet-stream; name=" + name + "\r\n")
	buf.WriteString("Content-Disposition: attachment; filename=" + name + "\r\n")
	buf.WriteString(email.TextBody + "\r\n")
	return []byte(buf.String())
}

// assertNoHeaderInjection rejects CR/LF/NUL only: the hard
// header-injection guard, deliberately NOT a C0/DEL scrub.
func assertNoHeaderInjection(field, value string) error {
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("header %q contains CR/LF/NUL", field)
	}
	return nil
}

// quoteParamValue keeps its strong "quote" name and its body without
// control-range evidence: it escapes only '"' and '\\', so the C0 range
// and DEL pass through — the classification fix this fixture pins.
func quoteParamValue(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := range s {
		c := s[i]
		if c == '"' || c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// scrubHeader is the fix posture: a byte walk whose comparisons name
// the control range, so the name and the body agree.
func scrubHeader(s string) string {
	var b strings.Builder
	for i := range s {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}
