package email

import (
	"errors"
	"strings"
	"testing"
)

// A Headers key reached the wire as the field name with no check but
// CR/LF. A name with a colon or a space wrote a line that reads as some
// other field ("X-A: b X-Y: v"), a leading space folded the line into
// the header above it, and a key naming a field the Email itself carries
// (To, Cc, Bcc, From, Subject) gave the message two of that field: a
// reader displays one recipient list while the envelope delivers to the
// other, and Headers["Bcc"] published the blind list the sender
// deliberately never writes. Property: every custom header line names a
// field-name RFC 5322 allows, and no field appears twice.
func TestHeadersKeyIsOneFieldName(t *testing.T) {
	base := Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", TextBody: "x"}
	refused := []string{
		"To", "cc", "BCC", "From", "subject",
		"",
		"X-A: forged",
		"X A",
		" X-Folded",
		"X-Tab\x09",
		"X-é",
	}
	for _, k := range refused {
		e := base
		e.Headers = map[string]string{k: "v"}
		if _, err := buildMessage(e); !errors.Is(err, ErrSendFailed) {
			t.Errorf("Headers[%q]: %v, want ErrSendFailed", k, err)
		}
	}
	e := base
	e.Headers = map[string]string{"Reply-To": "c@example.com", "X-Tag_1.v2": "v", "List-Unsubscribe": "<mailto:u@example.com>"}
	msg, err := buildMessage(e)
	if err != nil {
		t.Fatalf("legal custom headers refused: %v", err)
	}
	if !strings.Contains(string(msg), "\r\nX-Tag_1.v2: v\r\n") {
		t.Errorf("custom header missing from:\n%s", msg)
	}
}
