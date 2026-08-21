package dotenv

import (
	"strings"
	"testing"
)

// Property: a parse error reports WHERE the file is wrong without echoing
// WHAT the file contains. A .env is by definition secrets; the error
// string travels to callers that log it verbatim (host apps wrap
// LoadAndApply in log.Fatal), so a malformed line's payload must not ride
// along into the log.
//
// The missing-'=' branch quoted the whole raw line. The realistic shape is
// a secret typed with a space or a missing equals, exactly the line whose
// value must not reach stderr.
func TestParseErrorDoesNotEchoValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"missing equals leaks the secret", "API_TOKEN sk-live-QQ77zzWWvvUU11223344\n"},
		{"space instead of equals", "PASSWORD hunter2 with spaces\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.in))
			if err == nil {
				t.Fatalf("expected a parse error for %q", tc.in)
			}
			// The raw payload (everything after the key) must not appear.
			if strings.Contains(err.Error(), "sk-live-QQ77") ||
				strings.Contains(err.Error(), "hunter2") {
				t.Errorf("SECURITY: [dotenv] parse error echoes file content: %v", err)
			}
		})
	}
}

// The happy-path error surfaces that already avoid echoing: an invalid key
// names the key only, an unterminated quote names the shape only. Pin them
// so a refactor cannot regress the branches that are already clean.
func TestParseErrorNamesShapeNotContent(t *testing.T) {
	_, err := Parse(strings.NewReader("BAD-KEY=\"some long secret value\""))
	if err == nil {
		t.Fatal("expected invalid-key error")
	}
	if strings.Contains(err.Error(), "some long secret value") {
		t.Errorf("invalid-key error echoes the value: %v", err)
	}

	_, err = Parse(strings.NewReader("MSG=\"never closed\n"))
	if err == nil {
		t.Fatal("expected unterminated-quote error")
	}
	if strings.Contains(err.Error(), "never closed") {
		t.Errorf("unterminated-quote error echoes the value: %v", err)
	}
}
