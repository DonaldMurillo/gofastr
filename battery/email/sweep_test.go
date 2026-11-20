package email

import (
	"strings"
	"testing"
)

func TestInvisibleHeaderCodepointIsAnError(t *testing.T) {
	_, err := buildMessage(Email{From: "a@example.com", To: []string{"b@example.com"}, Subject: "Team ‮ Alice", TextBody: "x"})
	if err == nil {
		t.Fatal("buildMessage accepted a Subject holding U+202E")
	}
}

func TestRedactBodyDropsBearerValue(t *testing.T) {
	if got := RedactBody("call with Authorization: Bearer abc.def-123"); strings.Contains(got, "abc.def-123") {
		t.Fatalf("RedactBody kept the bearer token: %q", got)
	}
}

func TestAttachmentContentTypeKeepsOnlyTheBaseType(t *testing.T) {
	msg, err := buildMessage(Email{
		From: "a@example.com", To: []string{"b@example.com"}, TextBody: "x",
		Attachments: []Attachment{{Filename: "r.csv", ContentType: `text/csv; name="evil.exe"`, Content: []byte("1")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(msg), "evil.exe") {
		t.Fatalf("a caller's Content-Type parameter reached the wire:\n%s", msg)
	}
}
