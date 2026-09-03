//go:build red

// RED TESTS — open findings, 2026-09-02 round-2 adversarial pass (tests-only; no fix applied).
// Property: header lines of the outgoing SMTP message must carry no C0 control
// bytes (0x00–0x1F) or DEL (0x7F) — the only framing bytes the builder itself
// writes are the CRLF line terminators. Same scrubbing property the log side
// pins via core/middleware scrubControlBytes; email headers are read by MUAs,
// spam filters, and archive tooling that render control bytes verbatim.
// Surfaces: battery/email/smtp.go:buildMessage (assertNoHeaderInjection on
// From/To/Cc/Bcc/Subject/custom headers; quoteParamValue on attachment
// name=/filename= params). Severity: production-facing (outgoing mail path).
// Finding: assertNoHeaderInjection rejects only CR/LF/NUL, so 0x01–0x08, 0x0B,
// 0x0C, 0x0E–0x1F and 0x7F pass into every header value verbatim, and
// quoteParamValue strips the same narrow set only, so they also pass into the
// quoted MIME parameters. An ESC/BEL terminal-title sequence in Subject or a
// DEL in a custom header reaches the wire unchanged.
// Fix direction: extend the reject/strip set to the full C0 range + DEL (either
// refuse to serialise, or strip/percent-encode before writing the header) in
// assertNoHeaderInjection and quoteParamValue; keep the CR/LF/NUL refusal as
// the hard header-injection guard. Both tests accept either fix shape: they
// pass when buildMessage refuses the message OR emits clean header lines.
package email

import (
	"strings"
	"testing"
)

// assertLineNoCtrl fails when a header line carries any C0 control byte or DEL.
// Callers pass lines with the trailing CR/LF already trimmed, so ANY control
// byte found is message content, not framing.
func assertLineNoCtrl(t *testing.T, line string) {
	t.Helper()
	for i := range len(line) {
		if b := line[i]; b < 0x20 || b == 0x7f {
			t.Errorf("SECURITY: [email-ctrl] header line %q carries control byte 0x%02X at offset %d — C0/DEL (outside CR/LF/NUL) is smuggled into the outgoing SMTP message; assertNoHeaderInjection/quoteParamValue cover only CR/LF/NUL", line, b, i)
			return
		}
	}
}

// TestEmailRedStripsHeaderControlBytes: C0 controls other than CR/LF/NUL and
// DEL must not reach From/To/Subject/custom header lines. VT in From, FF in To,
// an ESC…BEL terminal-title sequence in Subject, DEL in a custom header.
func TestEmailRedStripsHeaderControlBytes(t *testing.T) {
	msg, err := buildMessage(Email{
		From:     "from\x0b@example.com",
		To:       []string{"to\x0c@example.com"},
		Subject:  "a\x1b]0;pwn\x07b",
		Headers:  map[string]string{"X-Custom": "v\x7fal"},
		TextBody: "body",
	})
	if err != nil {
		return // refused outright: property satisfied by rejection
	}

	// Scan the top header block: everything before the first blank line.
	for _, ln := range strings.Split(string(msg), "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if ln == "" {
			break // end of headers
		}
		assertLineNoCtrl(t, ln)
	}
}

// TestEmailRedStripsParamControlBytes: attachment filename parameters
// (Content-Type name= / Content-Disposition filename=) must not carry C0/DEL.
// quoteParamValue strips only CR/LF/NUL, so SOH, ESC, and DEL pass through
// into both quoted parameters today.
func TestEmailRedStripsParamControlBytes(t *testing.T) {
	msg, err := buildMessage(Email{
		From:     "f@example.com",
		To:       []string{"t@example.com"},
		Subject:  "s",
		TextBody: "b",
		Attachments: []Attachment{{
			Filename: "re\x01port\x1b[2K\x7f.csv",
			Content:  []byte("data"),
		}},
	})
	if err != nil {
		return // refused outright: property satisfied by rejection
	}

	// Part headers live after the top block, so scan every line that is one.
	for _, ln := range strings.Split(string(msg), "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if strings.HasPrefix(ln, "Content-Type:") || strings.HasPrefix(ln, "Content-Disposition:") {
			assertLineNoCtrl(t, ln)
		}
	}
}
