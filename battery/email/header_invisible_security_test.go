package email

import (
	"bytes"
	"strings"
	"testing"
)

// SECURITY TEST — fixed 2026-09-05, red-probe round 4.
// Family: F25 Bidi, invisible, and confusable characters
// Property: a header value built from caller-influenced data carries no
// invisible, bidi, or C1 codepoints — the wire-side contract
// assertNoHeaderInjection already declared for C0+DEL ("MUA, spam filters
// and archive tooling render header bytes verbatim... smuggled onto the
// wire") extended to the codepoints that spoof the rendered header instead
// of breaking the line.
// Surfaces: smtp.go::assertNoHeaderInjection (refuses C0/DEL plus the C1
// block in both the UTF-8 and raw-byte spellings and the invisible/bidi
// set via core/textsafe), smtp.go::scrubHeaderValue (strips C1+invisible,
// percent-encodes C0/DEL — the wire-side backstop), smtp.go::buildMessage
// (Subject/From/To/Cc/Bcc/custom-header slots), smtp.go::quoteParamValue
// (attachment filename slot, same widened strip).
// Fix: the assert refuses the whole spoofing set and invalid UTF-8; the
// scrub drops invisible/C1 runes so nothing that slips a future gate can
// serialise a reversed or hidden header payload.

// TestHeaderRefusesInvisibleChars builds one message per invisible shape; the
// serialiser must refuse or scrub each, and a clean subject must still build.
func TestHeaderRefusesInvisibleChars(t *testing.T) {
	bad := []struct {
		name string
		r    rune
	}{
		{"rlo-override", '\u202E'},
		{"zero-width-space", '\u200B'},
		{"bom", '\uFEFF'},
		{"c1-osc-8bit", '\u009B'},
	}
	for _, tc := range bad {
		msg, err := buildMessage(Email{
			From:     "noreply@example.com",
			To:       []string{"user@example.com"},
			Subject:  "Team " + string(tc.r) + " Alice",
			TextBody: "hello",
		})
		if err == nil && bytes.ContainsRune(msg, tc.r) {
			t.Errorf("SECURITY: [email-header-invisible] Subject with %s (U+%04X) serialised onto the wire verbatim: %q", tc.name, tc.r, firstHeaderLine(msg))
		}
	}

	// Positive control: a clean subject builds fine.
	if _, err := buildMessage(Email{
		From:     "noreply@example.com",
		To:       []string{"user@example.com"},
		Subject:  "Team Alice",
		TextBody: "hello",
	}); err != nil {
		t.Fatalf("positive control: clean subject refused: %v", err)
	}
}

// firstHeaderLine trims the message to its Subject line for a readable
// failure message.
func firstHeaderLine(msg []byte) string {
	s := string(msg)
	if i := strings.Index(s, "Subject:"); i >= 0 {
		if j := strings.IndexByte(s[i:], '\n'); j > 0 {
			return s[i : i+j]
		}
	}
	return s
}
