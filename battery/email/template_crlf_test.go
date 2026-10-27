package email

import (
	"testing"
	"testing/fstest"
)

// A template file saved with CRLF line endings (a Windows editor) used to
// keep the "\r" on the subject parsed from its first line, and buildMessage
// refuses a CR in a header as injection: every send of that template
// failed.
func TestLoadFromFSCRLFSubject(t *testing.T) {
	fsys := fstest.MapFS{
		"welcome.txt":  {Data: []byte("Subject: Welcome {{.Name}}\r\nHello {{.Name}},\r\nwelcome aboard.\r\n")},
		"receipt.html": {Data: []byte("Subject: Your receipt\r\n<p>Thanks</p>\r\n")},
	}
	templates, err := LoadFromFS(fsys)
	if err != nil {
		t.Fatalf("LoadFromFS: %v", err)
	}

	welcome := templates["welcome"]
	if welcome.Subject != "Welcome {{.Name}}" {
		t.Fatalf("subject %q, want %q", welcome.Subject, "Welcome {{.Name}}")
	}
	if want := "Hello {{.Name}},\r\nwelcome aboard.\r\n"; welcome.TextBody != want {
		t.Fatalf("text body %q, want %q", welcome.TextBody, want)
	}
	if got := templates["receipt"].Subject; got != "Your receipt" {
		t.Fatalf("html-template subject %q, want %q", got, "Your receipt")
	}

	email, err := Execute(welcome, map[string]any{"Name": "Ada"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	email.From, email.To = "a@example.com", []string{"b@example.com"}
	if _, err := buildMessage(email); err != nil {
		t.Fatalf("a CRLF template's message was refused: %v", err)
	}
}
