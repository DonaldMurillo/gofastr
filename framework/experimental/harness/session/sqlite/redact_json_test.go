package sqlite

import (
	"encoding/json"
	"strings"
	"testing"
)

// The redactor runs on the decoded string fields, not on the serialized
// JSON: a secret at the end of a string used to take the closing quote
// with it, and a quoted value hid behind its \" escape. Either way the
// stored event no longer parsed and export dropped it.
func TestRedactKeepsPayloadValidJSON(t *testing.T) {
	cases := []struct{ in, secret string }{
		{"ZAI_API_KEY=4f8cAbCdEfNoP", "4f8cAbCdEfNoP"},
		{`ZAI_API_KEY="4f8cAbCdEfNoPq" next`, "4f8cAbCdEfNoPq"}, // not-a-secret: redactor fixture
		{"line\nFOO_TOKEN=abcdef123456", "abcdef123456"},
	}
	for _, tc := range cases {
		s := newStore(t)
		payload := appendTextDelta(t, s, tc.in)
		if !json.Valid([]byte(payload)) {
			t.Fatalf("stored payload is not JSON: %s", payload)
		}
		var got struct{ Text string }
		if err := json.Unmarshal([]byte(payload), &got); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(got.Text, tc.secret) {
			t.Errorf("secret survived: %q", got.Text)
		}
		if !strings.Contains(got.Text, "«redacted:secret-assignment»") {
			t.Errorf("no marker in %q", got.Text)
		}
	}
}
