package email

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

// Non-ASCII header text goes out as RFC 2047 encoded words and a
// non-ASCII attachment filename as an RFC 2231 parameter (2026-10-27
// bug bash E4): the localized subject, a display name and a filename
// were written as raw UTF-8, which a server without SMTPUTF8 rejects or
// mangles. A reader decodes each back to the text the caller gave.
func TestNonASCIIHeadersAreEncoded(t *testing.T) {
	raw, err := buildMessage(Email{
		From:     "José Pérez <jose@example.com>",
		To:       []string{"Zoë <zoe@example.com>", "plain@example.com"},
		CC:       []string{`"Ünal, Ada" <ada@example.com>`},
		Subject:  "¡Confirma tu correo!",
		TextBody: "hi",
		Attachments: []Attachment{
			{Filename: `résumé "v2".pdf`, ContentType: "application/pdf", Content: []byte("x")},
		},
	})
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	for i, c := range raw {
		if c >= 0x80 {
			t.Fatalf("byte %d is 0x%02x: the message is not ASCII\n%s", i, c, raw)
		}
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	var dec mime.WordDecoder
	if got, err := dec.DecodeHeader(msg.Header.Get("Subject")); err != nil || got != "¡Confirma tu correo!" {
		t.Errorf("Subject decodes to %q (%v)", got, err)
	}
	from, err := mail.ParseAddress(msg.Header.Get("From"))
	if err != nil || from.Name != "José Pérez" || from.Address != "jose@example.com" {
		t.Errorf("From = %+v (%v) from %q", from, err, msg.Header.Get("From"))
	}
	to, err := mail.ParseAddressList(msg.Header.Get("To"))
	if err != nil || len(to) != 2 || to[0].Name != "Zoë" || to[1].Address != "plain@example.com" {
		t.Errorf("To = %+v (%v) from %q", to, err, msg.Header.Get("To"))
	}
	cc, err := mail.ParseAddressList(msg.Header.Get("Cc"))
	if err != nil || len(cc) != 1 || cc[0].Name != "Ünal, Ada" || cc[0].Address != "ada@example.com" {
		t.Errorf("Cc = %+v (%v) from %q", cc, err, msg.Header.Get("Cc"))
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("content-type: %v", err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var names []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		if p.FileName() != "" {
			_, ctp, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
			if err != nil {
				t.Fatalf("attachment content-type: %v", err)
			}
			names = append(names, p.FileName(), ctp["name"])
		}
	}
	if want := `résumé "v2".pdf`; len(names) != 2 || names[0] != want || names[1] != want {
		t.Errorf("attachment filename, name = %q, want %q twice", names, want)
	}
}

// The RFC 2231 form drops what quoteParamValue drops (a control byte, a
// bidi override) and percent-encodes everything outside attribute-char,
// so a quote or semicolon cannot end the parameter. buildMessage never
// reaches the strip with such a name (assertNoHeaderInjection refuses
// it first); this pins the wire guarantee on its own.
func TestEncodeParamStripsAndEscapes(t *testing.T) {
	got := encodeParam("filename", "é\x01‮\"; x=y.pdf")
	if want := `filename*=utf-8''%C3%A9%22%3B%20x%3Dy.pdf`; got != want {
		t.Fatalf("encodeParam = %q, want %q", got, want)
	}
}

// ASCII header values keep the bytes they had: only text that needs an
// encoding gets one.
func TestASCIIHeadersStayAsWritten(t *testing.T) {
	raw, err := buildMessage(Email{
		From:     "Jane Doe <jane@example.com>",
		To:       []string{"Bob <bob@example.com>"},
		Subject:  "Confirm your email",
		TextBody: "hi",
		Attachments: []Attachment{
			{Filename: "report.pdf", ContentType: "application/pdf", Content: []byte("x")},
		},
	})
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	for _, line := range []string{
		"From: Jane Doe <jane@example.com>\r\n",
		"To: Bob <bob@example.com>\r\n",
		"Subject: Confirm your email\r\n",
		`Content-Disposition: attachment; filename="report.pdf"` + "\r\n",
	} {
		if !strings.Contains(string(raw), line) {
			t.Errorf("message lacks %q:\n%s", line, raw)
		}
	}
}
