package email

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
)

// mimeNode is one parsed MIME entity: its media type and either its
// child parts (multipart/*) or its body.
type mimeNode struct {
	mediaType string
	parts     []mimeNode
	body      string
}

// shape renders the tree as "multipart/mixed[multipart/alternative[text/plain,text/html],application/pdf]".
func (n mimeNode) shape() string {
	if len(n.parts) == 0 {
		return n.mediaType
	}
	kids := make([]string, len(n.parts))
	for i, p := range n.parts {
		kids[i] = p.shape()
	}
	return n.mediaType + "[" + strings.Join(kids, ",") + "]"
}

// leaves returns the non-multipart entities of n, depth first.
func leaves(n mimeNode) []mimeNode {
	if len(n.parts) == 0 {
		return []mimeNode{n}
	}
	var out []mimeNode
	for _, p := range n.parts {
		out = append(out, leaves(p)...)
	}
	return out
}

// parseMIME parses a built message the way a mail client does.
func parseMIME(t *testing.T, raw []byte) mimeNode {
	t.Helper()
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read message: %v\n%s", err, raw)
	}
	return parseEntity(t, textproto.MIMEHeader(msg.Header), msg.Body)
}

func parseEntity(t *testing.T, h textproto.MIMEHeader, body io.Reader) mimeNode {
	t.Helper()
	mt, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		t.Fatalf("content-type %q: %v", h.Get("Content-Type"), err)
	}
	n := mimeNode{mediaType: mt}
	if !strings.HasPrefix(mt, "multipart/") {
		b, _ := io.ReadAll(body)
		n.body = string(b)
		return n
	}
	mr := multipart.NewReader(body, params["boundary"])
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			return n
		}
		if err != nil {
			t.Fatalf("next part of %s: %v", mt, err)
		}
		n.parts = append(n.parts, parseEntity(t, p.Header, p))
	}
}

// A text+HTML message is two renderings of one body: multipart/
// alternative, plain text first and HTML last (RFC 2046 §5.1.4, the last
// part is the preferred one). multipart/mixed told clients to show both,
// one after the other.
func TestTextAndHTMLAreAlternative(t *testing.T) {
	att := Attachment{Filename: "invoice.pdf", ContentType: "application/pdf", Content: []byte("%PDF-1.4")}
	cases := []struct {
		name  string
		email Email
		want  string
	}{
		{"text and html", Email{TextBody: "plain", HTMLBody: "<p>rich</p>"},
			"multipart/alternative[text/plain,text/html]"},
		{"text, html and attachment", Email{TextBody: "plain", HTMLBody: "<p>rich</p>", Attachments: []Attachment{att}},
			"multipart/mixed[multipart/alternative[text/plain,text/html],application/pdf]"},
		{"text and attachment", Email{TextBody: "plain", Attachments: []Attachment{att}},
			"multipart/mixed[text/plain,application/pdf]"},
		{"html and attachment", Email{HTMLBody: "<p>rich</p>", Attachments: []Attachment{att}},
			"multipart/mixed[text/html,application/pdf]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.email
			e.From, e.To, e.Subject = "a@example.com", []string{"b@example.com"}, "s"
			raw, err := buildMessage(e)
			if err != nil {
				t.Fatalf("buildMessage: %v", err)
			}
			tree := parseMIME(t, raw)
			if got := tree.shape(); got != tc.want {
				t.Fatalf("MIME structure %s, want %s\n%s", got, tc.want, raw)
			}
			// The bodies survive the nesting intact.
			for _, leaf := range leaves(tree) {
				switch leaf.mediaType {
				case "text/plain":
					if strings.TrimSpace(leaf.body) != "plain" {
						t.Fatalf("text part body %q, want %q", leaf.body, "plain")
					}
				case "text/html":
					if strings.TrimSpace(leaf.body) != "<p>rich</p>" {
						t.Fatalf("html part body %q, want %q", leaf.body, "<p>rich</p>")
					}
				}
			}
		})
	}
}

// A body line equal to either delimiter, the nested alternative
// boundary's included, is refused. The boundaries are random, so the
// test fixes them to know what to forge.
func TestNestedBoundaryCannotBeForged(t *testing.T) {
	orig := newBoundary
	t.Cleanup(func() { newBoundary = orig })
	var n int
	newBoundary = func() (string, error) {
		n++
		return "fixed-boundary-" + string(rune('0'+n)), nil
	}

	att := []Attachment{{Filename: "a.txt", ContentType: "text/plain", Content: []byte("x")}}
	build := func(text string) error {
		n = 0
		_, err := buildMessage(Email{
			From: "a@example.com", To: []string{"b@example.com"}, Subject: "s",
			TextBody: text, HTMLBody: "<p>rich</p>", Attachments: att,
		})
		return err
	}
	if err := build("plain"); err != nil {
		t.Fatalf("benign body refused: %v", err)
	}
	for _, b := range []string{"fixed-boundary-1", "fixed-boundary-2"} {
		if err := build("x\r\n--" + b + "\r\nContent-Type: text/html\r\n\r\n<phish/>"); err == nil {
			t.Fatalf("a body forging the delimiter --%s was serialized", b)
		}
	}
}
