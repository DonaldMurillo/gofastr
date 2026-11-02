package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/control"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/ids"
)

// TestHarnessKeyMaterialRedactedAtPersist pins the harness's own key
// material. GOFASTR_HARNESS_PASSPHRASE and GOFASTR_HARNESS_MACHINE_KEY
// unlock every credential in the credstore, and both sit in the
// harness's environment, which every Bash child inherits: a tool result
// from `env`, `printenv` or a quiet-mode `cat /proc/self/environ` (no
// prompt) carries them. The assignment pattern knew API_KEY, TOKEN,
// SECRET, PASSWORD and ACCESS_KEY, so both landed in events.payload,
// and in every export bundle, verbatim.
func TestHarnessKeyMaterialRedactedAtPersist(t *testing.T) {
	cases := []struct{ name, in, keep string }{
		{"passphrase", "GOFASTR_HARNESS_PASSPHRASE=correct-horse-battery-staple", "correct-horse-battery-staple"},
		{"machine key", "GOFASTR_HARNESS_MACHINE_KEY=8f14e45fceea167a5a36dedd4bea2543", "8f14e45fceea167a5a36dedd4bea2543"},
		{"environ block", "HOME=/root\x00GOFASTR_HARNESS_PASSPHRASE=correct-horse-battery-staple\x00PATH=/bin", "correct-horse-battery-staple"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := appendTextDelta(t, newStore(t), tc.in)
			if strings.Contains(payload, tc.keep) {
				t.Errorf("secret survived redaction at persist: payload=%s", payload)
			}
		})
	}
}

// TestSecretKeyedJSONValueRedactedAtPersist pins a secret passed as a
// JSON object member. The redactors run on each decoded string of a
// payload separately, so a Bash call's env map {"ZAI_API_KEY": "<v>"}
// reached them as the key and the value apart: the assignment pattern
// needs NAME=value in one string and never matched, and a key without
// an sk- prefix (ZAI's <id>.<secret>) was stored verbatim in the
// ToolCallStarted args.
func TestSecretKeyedJSONValueRedactedAtPersist(t *testing.T) {
	const secret = "4f8c2a1be9d7406fbc3a90fd11e2.aBcDeFgHiJkLmNoP"
	args, err := json.Marshal(map[string]any{
		"cmd": "./deploy.sh",
		"env": map[string]string{"ZAI_API_KEY": secret, "REGION": "eu-west-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := newStore(t)
	sess := ids.NewSessionID()
	env, err := control.EncodeEvent(1, control.ToolCallStarted{
		CallID: ids.NewCallID(), Tool: "Bash", Args: args, Mutating: true,
	}, sess, ids.NewClientID(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	got, err := s.EventsSince(context.Background(), sess, 0, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("EventsSince: %v (%d events)", err, len(got))
	}
	payload := string(got[0].Payload)
	if strings.Contains(payload, secret) {
		t.Errorf("secret-keyed JSON value survived redaction: payload=%s", payload)
	}
	// The benign member and the payload's shape survive.
	if !strings.Contains(payload, "eu-west-1") || !json.Valid(got[0].Payload) {
		t.Errorf("redaction damaged the payload: %s", payload)
	}
}
