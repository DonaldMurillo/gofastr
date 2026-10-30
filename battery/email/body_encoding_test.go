package email

import (
	"bytes"
	"errors"
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
)

// A body went on the wire as written (2026-10-27 E6): a template saved
// on Unix put bare LFs under CRLF headers (RFC 5321 §2.3.8), and a
// minified one-line HTML body passed the 998-octet line limit
// (§4.5.3.1.6), which relays reject or break mid-tag.
func TestBodyLinesAreCRLFAndShort(t *testing.T) {
	long := "<p>" + strings.Repeat("ab=cd ", 400) + "</p>"
	text := "line one\nline two\rline three\r\nline four"
	cases := map[string]Email{
		"text only":      {TextBody: text},
		"html only":      {HTMLBody: long},
		"text and html":  {TextBody: text, HTMLBody: long},
		"with an attach": {TextBody: text, HTMLBody: long, Attachments: []Attachment{{Filename: "a.txt", Content: []byte("x")}}},
	}
	for name, e := range cases {
		t.Run(name, func(t *testing.T) {
			e.From, e.To, e.Subject = "a@example.com", []string{"b@example.com"}, "s"
			raw, err := buildMessage(e)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(raw), "\r\n") {
				if strings.ContainsAny(line, "\r\n") {
					t.Fatalf("line %d has a bare CR or LF: %q", i, line)
				}
				if len(line) > 998 {
					t.Fatalf("line %d is %d octets, past the 998 limit: %.80q", i, len(line), line)
				}
			}
			// Every text part says how it is encoded, and decodes back to
			// the body with its line endings made CRLF.
			tree := parseMIME(t, raw)
			for _, leaf := range leaves(tree) {
				var want string
				switch leaf.mediaType {
				case "text/plain":
					want = "line one\r\nline two\r\nline three\r\nline four"
				case "text/html":
					want = long
				default:
					continue
				}
				if leaf.encoding != "quoted-printable" {
					t.Fatalf("%s part Content-Transfer-Encoding %q, want quoted-printable", leaf.mediaType, leaf.encoding)
				}
				if got := strings.TrimSuffix(leaf.body, "\r\n"); got != want {
					t.Fatalf("%s part decodes to %.120q, want %.120q", leaf.mediaType, got, want)
				}
			}
		})
	}
}

// A single-part message declares MIME, or its Content-Transfer-Encoding
// header means nothing (RFC 2045 §4).
func TestSinglePartDeclaresMIME(t *testing.T) {
	raw, err := buildMessage(Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", TextBody: "héllo"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("MIME-Version") != "1.0" || msg.Header.Get("Content-Transfer-Encoding") != "quoted-printable" {
		t.Fatalf("headers %v, want MIME-Version 1.0 and quoted-printable", msg.Header)
	}
	body, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil || string(body) != "héllo" {
		t.Fatalf("body %q (%v), want héllo", body, err)
	}
}

// The sender writes the MIME structure headers, so a caller's copy would
// give the message two that disagree.
func TestHeadersCannotNameMIMEStructure(t *testing.T) {
	for _, k := range []string{"Content-Type", "content-transfer-encoding", "MIME-Version"} {
		_, err := buildMessage(Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", TextBody: "x", Headers: map[string]string{k: "base64"}})
		if !errors.Is(err, ErrSendFailed) {
			t.Errorf("Headers[%q]: %v, want ErrSendFailed", k, err)
		}
	}
	if _, err := buildMessage(Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", TextBody: "x", Headers: map[string]string{"X-Content-Type": "y"}}); err != nil {
		t.Errorf("a header that only contains the name was refused: %v", err)
	}
}

// Soft line breaks can put text from the middle of a raw line at the
// start of a wire line, and a line that only starts with the delimiter
// still ends the part (RFC 2046 §5.1.1). The check reads what is sent.
func TestBoundaryCheckReadsEncodedLines(t *testing.T) {
	orig := newBoundary
	t.Cleanup(func() { newBoundary = orig })
	newBoundary = func() (string, error) { return "fixed-boundary", nil }
	build := func(text string) error {
		_, err := buildMessage(Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", TextBody: text, HTMLBody: "<p>x</p>"})
		return err
	}
	if err := build("plain --fixed-boundary in a sentence"); err != nil {
		t.Fatalf("a delimiter mid-line, short line: %v", err)
	}
	wrapped := strings.Repeat("a", 75) + "--fixed-boundary"
	if !strings.Contains(qpBody(wrapped), "=\r\n--fixed-boundary") {
		t.Fatalf("precondition: the soft break should land before the delimiter: %q", qpBody(wrapped))
	}
	for name, body := range map[string]string{
		"wrapped onto a line": wrapped,
		"line prefix":         "--fixed-boundary-and-more",
	} {
		if err := build(body); err == nil {
			t.Errorf("%s: a body that puts the delimiter at a line start was sent", name)
		}
	}
}
