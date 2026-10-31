package email

import (
	"bytes"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"
)

// headerLines returns every header line of msg named name (ASCII case
// folded, the way a mail parser reads field names).
func headerLines(msg, name string) []string {
	head, _, _ := strings.Cut(msg, "\r\n\r\n")
	var out []string
	for _, line := range strings.Split(head, "\r\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && isASCII(k) && strings.EqualFold(k, name) {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// A sent message carries Date and a random Message-ID on the sender's
// domain (RFC 5322 3.6.1, 3.6.4), so direct delivery, threading and
// dedupe have them (2026-10-27 E5).
func TestMessageHasDateAndMessageID(t *testing.T) {
	e := Email{From: "App <noreply@example.com>", To: []string{"a@b.test"}, Subject: "hi", TextBody: "x"}
	before := time.Now().Add(-time.Second)
	msg, err := buildMessage(e)
	if err != nil {
		t.Fatal(err)
	}
	dates := headerLines(string(msg), "Date")
	if len(dates) != 1 {
		t.Fatalf("Date lines = %q, want one", dates)
	}
	d, err := mail.ParseDate(dates[0])
	if err != nil || d.Before(before.Truncate(time.Second)) || d.After(time.Now().Add(time.Second)) {
		t.Fatalf("Date %q = %v, %v", dates[0], d, err)
	}
	ids := headerLines(string(msg), "Message-ID")
	re := regexp.MustCompile(`^<[0-9a-f]{32}@example\.com>$`)
	if len(ids) != 1 || !re.MatchString(ids[0]) {
		t.Fatalf("Message-ID lines = %q", ids)
	}
	again, err := buildMessage(e)
	if err != nil {
		t.Fatal(err)
	}
	if headerLines(string(again), "Message-ID")[0] == ids[0] {
		t.Fatal("two messages share a Message-ID")
	}
}

// A Date or Message-ID the caller sets in Headers replaces the stamped
// one, whatever its case, so the message never carries two.
func TestHeadersOverrideDateAndMessageID(t *testing.T) {
	e := Email{
		From: "noreply@example.com", To: []string{"a@b.test"}, Subject: "hi", TextBody: "x",
		Headers: map[string]string{"message-id": "<fixed@example.com>", "DATE": "Thu, 29 Oct 2026 10:00:00 +0000"},
	}
	msg, err := buildMessage(e)
	if err != nil {
		t.Fatal(err)
	}
	if ids := headerLines(string(msg), "Message-ID"); len(ids) != 1 || ids[0] != "<fixed@example.com>" {
		t.Fatalf("Message-ID lines = %q", ids)
	}
	if ds := headerLines(string(msg), "Date"); len(ds) != 1 || ds[0] != "Thu, 29 Oct 2026 10:00:00 +0000" {
		t.Fatalf("Date lines = %q", ds)
	}
}

// A homoglyph key (long s, U+017F, folds to "s") does not count as the
// caller's Message-ID, so the message still gets a real one.
func TestHomoglyphHeaderKeepsMessageID(t *testing.T) {
	msg, err := buildMessage(Email{
		From: "noreply@example.com", To: []string{"a@b.test"}, Subject: "hi", TextBody: "x",
		Headers: map[string]string{"Meſſage-ID": "<x@y>"},
	})
	if err != nil {
		return // refusing the key outright also keeps the real header
	}
	if ids := headerLines(string(msg), "Message-ID"); len(ids) != 1 || !strings.HasSuffix(ids[0], "@example.com>") {
		t.Fatalf("Message-ID lines = %q", ids)
	}
}

// A From whose domain is not a plain host name does not reach the
// Message-ID; the id falls back to localhost.
func TestMessageIDDomainFallback(t *testing.T) {
	for _, from := range []string{`"x" <"a b"@[127.0.0.1]>`, "noreply@exa_mple.com"} {
		msg, err := buildMessage(Email{From: from, To: []string{"a@b.test"}, Subject: "hi", TextBody: "x"})
		if err != nil {
			t.Fatalf("From %q: %v", from, err)
		}
		ids := headerLines(string(msg), "Message-ID")
		if len(ids) != 1 || !strings.HasSuffix(ids[0], "@localhost>") {
			t.Fatalf("From %q: Message-ID lines = %q", from, ids)
		}
	}
}

// The dev LogSender scrubs every line it prints, attachment names and
// bodies included: an ESC in a template or a file name never reaches
// the terminal (2026-10-27 E7).
func TestLogSenderScrubsBodiesAndAttachments(t *testing.T) {
	var buf bytes.Buffer
	err := NewLogSender(&buf).Send(t.Context(), Email{
		From: "a@b.test", To: []string{"c@d.test"}, Subject: "s",
		TextBody:    "line one\n\x1b[2Jline\ttwo\u009b\x9b\u202e",
		HTMLBody:    "<p>\x1b]0;title\x07</p>",
		Attachments: []Attachment{{Filename: "r\x1b[31m.txt", ContentType: "text/\x1bplain", Content: []byte("x")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.ContainsAny(out, "\x1b\x07\u009b\u202e") || strings.Contains(out, "\x9b") {
		t.Fatalf("control bytes reached the log: %q", out)
	}
	if !strings.Contains(out, "line one\n") || !strings.Contains(out, "\ttwo") {
		t.Fatalf("newlines or tabs were lost: %q", out)
	}
}
