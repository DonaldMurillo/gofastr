package email

import (
	"regexp"
	"strings"
	"testing"
)

// boundaryRe extracts the multipart boundary from a built message.
var boundaryRe = regexp.MustCompile(`boundary=(?:"([^"]+)"|([^\s;]+))`)

func messageBoundary(t *testing.T, msg []byte) string {
	t.Helper()
	m := boundaryRe.FindSubmatch(msg)
	if m == nil {
		t.Fatalf("no boundary found in message:\n%s", msg)
	}
	if len(m[1]) > 0 {
		return string(m[1])
	}
	return string(m[2])
}

// TestEmail_BodyCannotInjectMIMEPart asserts a body cannot smuggle a
// new MIME part or terminate the multipart container. Attack: a body
// (rendered from an attacker-influenced template) embeds a line equal
// to the boundary delimiter to inject a phishing part or truncate.
func TestEmail_BodyCannotInjectMIMEPart(t *testing.T) {
	// Happy path: a benign body that merely mentions dashes is fine.
	good := Email{
		From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi",
		TextBody: "line one\n--- a divider ---\nline two",
		HTMLBody: "<p>hello</p>",
	}
	if _, err := buildMessage(good); err != nil {
		t.Fatalf("benign body rejected: %v", err)
	}

	// Attack 1: body forges the historic fixed boundary delimiter.
	legacy := Email{
		From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi",
		TextBody: "intro\r\n--gofastr-boundary-12345\r\nContent-Type: text/html\r\n\r\n<phish>steal</phish>",
		HTMLBody: "<p>legit</p>",
	}
	assertBodyCannotInject(t, "legacy-fixed-boundary", legacy)

	// Attack 2: body tries to terminate the multipart container early.
	truncate := Email{
		From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi",
		TextBody: "intro\r\n--gofastr-boundary-12345--\r\ntrailing junk",
		HTMLBody: "<p>legit</p>",
	}
	assertBodyCannotInject(t, "early-termination", truncate)

	// Attack 3: same forgery but via the HTML body part.
	htmlInject := Email{
		From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi",
		TextBody: "legit",
		HTMLBody: "<p>hi</p>\r\n--gofastr-boundary-12345\r\nContent-Type: text/html\r\n\r\n<phish/>",
	}
	assertBodyCannotInject(t, "html-body-forgery", htmlInject)
}

// assertBodyCannotInject builds the message and fails if the body's
// forged delimiter ends up as a real boundary delimiter line.
func assertBodyCannotInject(t *testing.T, name string, email Email) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		msg, err := buildMessage(email)
		if err != nil {
			// Failing closed (rejecting the body) is an acceptable defense.
			return
		}
		b := messageBoundary(t, msg)
		// If the real boundary equals the predictable historic constant,
		// the body could forge it — injection succeeds.
		if b == "gofastr-boundary-12345" {
			t.Fatalf("SECURITY: [email] boundary is the predictable fixed constant — body can forge it")
		}
		// The body must never produce a line equal to the REAL delimiter.
		// A verbatim copy of the old fixed constant is inert because the
		// real boundary is random. Count structural delimiters: text +
		// html => 2 part-openers + 1 closer = 3. If the body had forged
		// the real delimiter we'd see extra delimiter lines.
		realDelim := "--" + b
		var delimCount int
		for _, line := range strings.Split(string(msg), "\r\n") {
			trimmed := strings.TrimRight(line, "\r")
			if trimmed == realDelim || trimmed == realDelim+"--" {
				delimCount++
			}
		}
		if delimCount != 3 {
			t.Fatalf("SECURITY: [email] body injected an extra real boundary delimiter: got %d delimiter lines, want 3:\n%s", delimCount, msg)
		}
	})
}

// TestEmail_FilenameCannotInjectMIMEParam asserts an attachment filename
// cannot break out of the quoted MIME parameter. Attack: a filename
// containing a bare double-quote terminates the quoted name/filename
// parameter and appends attacker-chosen parameters.
func TestEmail_FilenameCannotInjectMIMEParam(t *testing.T) {
	// Happy path: an ordinary filename round-trips.
	good := Email{
		From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi", TextBody: "b",
		Attachments: []Attachment{{Filename: "report.csv", ContentType: "text/csv", Content: []byte("a,b")}},
	}
	msg, err := buildMessage(good)
	if err != nil {
		t.Fatalf("benign attachment rejected: %v", err)
	}
	if !strings.Contains(string(msg), `filename="report.csv"`) {
		t.Fatalf("benign filename not emitted correctly:\n%s", msg)
	}

	cases := []struct {
		name string
		att  Attachment
	}{
		{"quote_breakout_filename", Attachment{
			Filename: `report.csv"; name="invoice.pdf`, ContentType: "text/csv", Content: []byte("x"),
		}},
		{"quote_breakout_exe", Attachment{
			Filename: `x"; filename="setup.exe`, ContentType: "text/csv", Content: []byte("x"),
		}},
		{"quote_in_contenttype", Attachment{
			Filename: "ok.csv", ContentType: `text/csv"; name="evil.exe`, Content: []byte("x"),
		}},
		{"backslash_quote", Attachment{
			Filename: `a\"; name="b`, ContentType: "text/csv", Content: []byte("x"),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := Email{
				From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi", TextBody: "b",
				Attachments: []Attachment{tc.att},
			}
			msg, err := buildMessage(email)
			if err != nil {
				// Rejecting the dangerous filename is an acceptable defense.
				return
			}
			// Parse each header line that carries the params; ensure the
			// attacker's injected extra parameter is not a live param.
			// A successful breakout produces an unescaped `"; name="` or
			// `"; filename="` sequence inside the header line.
			s := string(msg)
			for _, bad := range []string{`"; name="invoice.pdf"`, `"; filename="setup.exe"`, `"; name="evil.exe"`, `"; name="b"`} {
				if strings.Contains(s, bad) {
					t.Fatalf("SECURITY: [email] attachment filename/content-type broke out of quoted MIME param: found %q in:\n%s", bad, s)
				}
			}
		})
	}
}
