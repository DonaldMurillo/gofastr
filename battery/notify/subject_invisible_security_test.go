package notify

import (
	"context"
	"strings"
	"testing"
)

// SECURITY TEST — fixed 2026-09-05, red-probe round 4.
// Family: F25 Bidi, invisible, and confusable characters
// Property: a rendered value bound for a header slot carries no invisible or
// bidi codepoints — the same {{placeholder}} scrubbing contract
// stripHeaderUnsafe already declared for CR/LF/NUL ("a user-controlled
// {{placeholder}} can't inject ... when downstream transports treat Subject
// as a header value") extended to the characters that spoof rather than
// break the header.
// Surfaces: notify.go::MapTemplater.Render → stripHeaderUnsafe (Subject —
// now the C0/DEL/C1/invisible set via core/textsafe); the rendered Subject
// flows onward through EmailChannel into battery/email headers and into
// push-provider titles; LoggerChannel logs it verbatim.
// Fix: stripHeaderUnsafe drops the invisible/bidi set and the C1 control
// block alongside C0/DEL, so an RLO/ZWSP/BOM payload in a placeholder
// cannot reach a mail header or push title verbatim.

// TestSubjectScrubbedOfInvisible renders one template with four invisible
// attack shapes in a placeholder value; none may reach the Subject.
func TestSubjectScrubbedOfInvisible(t *testing.T) {
	tmpl := NewMapTemplater()
	tmpl.Set("acct.notice", "email", Template{
		Subject:  "Hello {{name}}",
		TextBody: "Hi {{name}}",
	})

	bad := []struct {
		name string
		r    rune
	}{
		{"rlo-override", '\u202E'},
		{"zero-width-space", '\u200B'},
		{"bom", '\uFEFF'},
		{"c1-osc-8bit", '\u009B'},
	}
	for _, tc := range bad {
		r, err := tmpl.Render(context.Background(), "acct.notice", "email",
			map[string]any{"name": "Alice" + string(tc.r) + "bob"})
		if err != nil {
			t.Fatalf("render with %s: %v", tc.name, err)
		}
		if strings.ContainsRune(r.Subject, tc.r) {
			t.Errorf("SECURITY: [notify-subject-invisible] Subject still carries %s (U+%04X): %q — an invisible/bidi codepoint in a user-controlled placeholder reaches the mail header verbatim", tc.name, tc.r, r.Subject)
		}
	}

	// Positive control: a clean value renders unchanged — the scrub must not
	// mutate legible subjects.
	r, err := tmpl.Render(context.Background(), "acct.notice", "email",
		map[string]any{"name": "Alicebob"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != "Hello Alicebob" {
		t.Fatalf("positive control: Subject = %q, want %q", r.Subject, "Hello Alicebob")
	}
}
