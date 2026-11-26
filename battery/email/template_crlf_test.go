package email

import (
	"os"
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

func TestLoadPairedTemplateSubjects(t *testing.T) {
	for _, tc := range []struct {
		name       string
		text, html string
		subject    string
		want       Template
	}{
		{
			name: "text subject wins",
			text: "Subject: Text subject\nplain body",
			html: "Subject: HTML subject\n<p>HTML body</p>",
			want: Template{Subject: "Text subject", TextBody: "plain body", HTMLBody: "<p>HTML body</p>"},
		},
		{
			name:    "subject file wins with CRLF",
			text:    "Subject: Text subject\r\nplain body\r\n",
			html:    "Subject: HTML subject\r\n<p>HTML body</p>\r\n",
			subject: " File subject\r\n",
			want:    Template{Subject: "File subject", TextBody: "plain body\r\n", HTMLBody: "<p>HTML body</p>\r\n"},
		},
		{
			name: "fall back to HTML subject",
			text: "Subject: \nplain body",
			html: "Subject: HTML subject\r\n<p>HTML body</p>\r\n",
			want: Template{Subject: "HTML subject", TextBody: "plain body", HTMLBody: "<p>HTML body</p>\r\n"},
		},
		{
			name: "HTML without a leading subject is unchanged",
			text: "Subject: Text subject\nplain body",
			html: "\r\nSubject: HTML content\r\n<p>HTML body</p>\r\n",
			want: Template{Subject: "Text subject", TextBody: "plain body", HTMLBody: "\r\nSubject: HTML content\r\n<p>HTML body</p>\r\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{
				"welcome.txt":  {Data: []byte(tc.text)},
				"welcome.html": {Data: []byte(tc.html)},
			}
			if tc.subject != "" {
				files["welcome.subject"] = &fstest.MapFile{Data: []byte(tc.subject)}
			}
			check := func(t *testing.T, templates map[string]Template, err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				if got := templates["welcome"]; got != tc.want {
					t.Errorf("template = %#v, want %#v", got, tc.want)
				}
			}
			t.Run("dir", func(t *testing.T) {
				dir := t.TempDir()
				if err := os.CopyFS(dir, files); err != nil {
					t.Fatal(err)
				}
				templates, err := LoadFromDir(dir)
				check(t, templates, err)
			})
			t.Run("fs", func(t *testing.T) {
				templates, err := LoadFromFS(files)
				check(t, templates, err)
			})
		})
	}
}

func TestLoadFromFSUTF8BOM(t *testing.T) {
	templates, err := LoadFromFS(fstest.MapFS{
		"welcome.txt":  {Data: []byte("\ufeffSubject: Welcome\nHello\n")},
		"welcome.html": {Data: []byte("\ufeff<p>Hello</p>")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := templates["welcome"]; got.Subject != "Welcome" || got.TextBody != "Hello\n" || got.HTMLBody != "<p>Hello</p>" {
		t.Fatalf("template = %+v", got)
	}
}
