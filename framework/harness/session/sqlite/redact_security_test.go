package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
)

// TestProviderKeyRedaction asserts the on-write redactor neutralises the
// provider API-key formats the harness itself resolves, so a tool-result
// echoing a bare key (e.g. "env | grep KEY") never lands in events.payload
// plaintext. Property: every harness-handled secret format is redacted at
// the persist surface.
func TestProviderKeyRedaction(t *testing.T) {
	cases := []struct {
		name string
		in   string
		keep string // a clearly-secret substring that must NOT survive
	}{
		{
			name: "openrouter sk-or key",
			in:   "OPENROUTER_API_KEY=sk-or-v1-0123456789abcdef0123456789abcdef0123456789abcdef",
			keep: "sk-or-v1-0123456789abcdef",
		},
		{
			name: "anthropic sk-ant key",
			in:   "export ANTHROPIC_API_KEY=sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			keep: "sk-ant-api03-AAAAAAAA",
		},
		{
			name: "zai assignment without sk prefix",
			in:   "ZAI_API_KEY=4f8c2a1be9d7406fbc3a90fd11e2.aBcDeFgHiJkLmNoP",
			keep: "4f8c2a1be9d7406fbc3a90fd11e2.aBcDeFgHiJkLmNoP",
		},
		{
			name: "generic secret assignment",
			in:   "FOO_SECRET=hunter2hunter2hunter2hunter2xx",
			keep: "hunter2hunter2hunter2hunter2xx",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			sess := ids.NewSessionID()
			env, err := control.EncodeEvent(1, control.TextDelta{Text: tc.in}, sess, ids.NewClientID(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.AppendEvent(context.Background(), env); err != nil {
				t.Fatal(err)
			}
			got, err := s.EventsSince(context.Background(), sess, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			payload := string(got[0].Payload)
			if strings.Contains(payload, tc.keep) {
				t.Errorf("secret survived redaction: payload=%s", payload)
			}
		})
	}

	// Happy path: an ordinary value that merely contains an = sign must not
	// be mangled into nothing — redaction is targeted, not destroy-all.
	t.Run("benign assignment preserved", func(t *testing.T) {
		s := newStore(t)
		sess := ids.NewSessionID()
		const benign = "PATH=/usr/local/bin"
		env, _ := control.EncodeEvent(1, control.TextDelta{Text: benign}, sess, ids.NewClientID(), time.Now())
		if err := s.AppendEvent(context.Background(), env); err != nil {
			t.Fatal(err)
		}
		got, _ := s.EventsSince(context.Background(), sess, 0, 0)
		if !strings.Contains(string(got[0].Payload), "/usr/local/bin") {
			t.Errorf("benign value clobbered: %s", got[0].Payload)
		}
	})
}
