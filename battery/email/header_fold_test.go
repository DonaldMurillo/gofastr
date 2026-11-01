package email

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

// Long header fields are folded (RFC 5322 2.2.3) so no line of the
// message passes 998 octets, and each reads back unchanged. A
// 1000-character subject went out as one line, which a relay may
// refuse or truncate. Encoded words are never split mid-character.
func TestLongHeadersFoldUnder998(t *testing.T) {
	words := strings.Repeat("word ", 300)
	cases := map[string]string{
		"spaced ascii":  strings.TrimSpace(words),
		"unbroken":      strings.Repeat("x", 1500),
		"non-ascii":     strings.Repeat("é", 1200),
		"mixed":         "Re: " + strings.Repeat("ü", 300) + " " + strings.Repeat("y", 1100) + " end",
		"emoji":         strings.Repeat("😀", 400),
		"q specials":    strings.Repeat("a=?_", 400),
		"short":         "Hello",
		"leading space": " x " + strings.Repeat("z", 1000),
		// Two long words in a row: RFC 2047 drops the space between two
		// adjacent encoded words, so the space must travel inside one.
		"two long words":            strings.Repeat("a", 80) + " " + strings.Repeat("b", 80),
		"long then long then short": strings.Repeat("a", 90) + "  " + strings.Repeat("b", 90) + " c",
	}
	longFile := strings.Repeat("ré", 400) + ".pdf"
	asciiFile := strings.Repeat("f", 1200) + ".txt"
	for name, subject := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := buildMessage(Email{
				From:     "a@example.com",
				To:       []string{strings.Repeat("x", 60) + "@example.com", strings.Repeat("y", 60) + "@example.com"},
				Subject:  subject,
				Headers:  map[string]string{"X-Note": strings.TrimSpace(words)},
				TextBody: "hi",
				Attachments: []Attachment{
					{Filename: longFile, ContentType: "application/pdf", Content: []byte("x")},
					{Filename: asciiFile, ContentType: "text/plain", Content: []byte("y")},
				},
			})
			if err != nil {
				t.Fatalf("buildMessage: %v", err)
			}
			for i, line := range strings.Split(string(raw), "\r\n") {
				if len(line) > 998 {
					t.Fatalf("line %d is %d octets: %.80q", i, len(line), line)
				}
			}
			msg, err := mail.ReadMessage(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("read message: %v", err)
			}
			var dec mime.WordDecoder
			got, err := dec.DecodeHeader(msg.Header.Get("Subject"))
			if want := strings.TrimSpace(subject); err != nil || got != want {
				t.Errorf("Subject decodes to %.80q (%v), want %.80q", got, err, want)
			}
			if got := msg.Header.Get("X-Note"); got != strings.TrimSpace(words) {
				t.Errorf("X-Note reads back as %.80q", got)
			}
			if to, err := mail.ParseAddressList(msg.Header.Get("To")); err != nil || len(to) != 2 {
				t.Errorf("To = %v (%v)", to, err)
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
				if p.FileName() == "" {
					continue
				}
				_, ctp, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
				if err != nil {
					t.Fatalf("attachment content-type: %v", err)
				}
				names = append(names, p.FileName(), ctp["name"])
			}
			want := []string{longFile, longFile, asciiFile, asciiFile}
			if strings.Join(names, "|") != strings.Join(want, "|") {
				t.Errorf("attachment names = %.120q", names)
			}
		})
	}
}
