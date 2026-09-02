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
	// be mangled into nothing, redaction is targeted, not destroy-all.
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

// appendTextDelta persists one attacker-shaped TextDelta and returns
// the payload round-tripped through the store, so every test below
// exercises the real on-write surface (AppendEvent) rather than the
// regex table in isolation.
func appendTextDelta(t *testing.T, s *Store, text string) string {
	t.Helper()
	sess := ids.NewSessionID()
	env, err := control.EncodeEvent(1, control.TextDelta{Text: text}, sess, ids.NewClientID(), time.Now())
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
	if len(got) == 0 {
		t.Fatal("no events stored")
	}
	return string(got[0].Payload)
}

// TestStoredSecretFormatsRedactedAtPersist pins the remaining formats
// the redactor's own coverage list names. Property: every secret
// format redact.go documents is neutralised at the persist surface —
// a tool result echoing any of them must never land in events.payload
// plaintext. (The provider-key formats are pinned above; these are the
// cloud/token formats: AWS keys, GitHub PATs in their real lengths,
// bearer headers, JWTs.)
func TestStoredSecretFormatsRedactedAtPersist(t *testing.T) {
	cases := []struct {
		name string
		in   string
		keep string // clearly-secret substring that must NOT survive
	}{
		{
			name: "aws access key",
			in:   "aws_access_key_id = AKIAIOSFODNN7EXAMPLE", // not-a-secret: synthetic fixture for a redaction/injection probe
			keep: "AKIAIOSFODNN7EXAMPLE",                     // not-a-secret: synthetic fixture for a redaction/injection probe
		},
		{
			name: "aws session key",
			in:   "creds: ASIAIOSFODNN7EXAMPLE token",
			keep: "ASIAIOSFODNN7EXAMPLE",
		},
		{
			name: "github classic pat (real 36-char body)",
			in:   "GITHUB_TOKEN=ghp_0123456789abcdefghijklmnopqrstuvwxyzABCD", // not-a-secret: synthetic fixture for a redaction/injection probe
			keep: "ghp_0123456789abcdefghijklmnopqrstuvwxyzABCD",              // not-a-secret: synthetic fixture for a redaction/injection probe
		},
		{
			name: "github oauth token",
			in:   "gho_0123456789abcdefghijklmnopqrstuvwxyzABC", // not-a-secret: synthetic fixture for a redaction/injection probe
			keep: "gho_0123456789abcdefghijklmnopqrstuvwxyzABC", // not-a-secret: synthetic fixture for a redaction/injection probe
		},
		{
			name: "github fine-grained pat",
			in:   "github_pat_11AAAAAAA0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			keep: "github_pat_11AAAAAAA0aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name: "bearer header",
			in:   "Authorization: Bearer dGhpcyBpcyBhIHNlY3JldCB0b2tlbg==",
			keep: "dGhpcyBpcyBhIHNlY3JldCB0b2tlbg==",
		},
		{
			name: "jwt triple",
			in:   "id_token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N", // not-a-secret: synthetic unsigned JWT shape, exists only to prove redaction
			keep: "dozjgNryP4J3jVmNHl0w5N",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := appendTextDelta(t, newStore(t), tc.in)
			if strings.Contains(payload, tc.keep) {
				t.Errorf("SECURITY: secret survived redaction at persist: payload=%s", payload)
			}
		})
	}
}

// TestPEMKeyMaterialRedactedAtPersist pins the private-key property:
// redact.go claims coverage of "PEM-headered private keys", but the
// pattern replaces only the BEGIN header line. The key MATERIAL — the
// base64 body, which is the actual secret — survives in full, so a
// tool result that cats a private key (a classic exfiltration move:
// `cat ~/.ssh/id_rsa`) stores the key in events.payload plaintext,
// header or no header.
//
// RED today: only "-----BEGIN RSA PRIVATE KEY-----" is replaced; the // not-a-secret: synthetic fixture for a redaction/injection probe
// body and the END line are stored verbatim.
func TestPEMKeyMaterialRedactedAtPersist(t *testing.T) {
	pem := "-----BEGIN RSA PRIVATE KEY-----\n" + // not-a-secret: synthetic fixture for a redaction/injection probe
		"MIIEpAIBAAKCAQEAx1yQGcXVpZXI0ISEhISEhISEhISEhISEhISEhISEh\n" +
		"kQ2h5Y9eZv7tG6fJ3n2cB8xW4vLpM1sK9dR7uT0eY5oP3iF8aD6sH4gU\n" +
		"-----END RSA PRIVATE KEY-----"
	payload := appendTextDelta(t, newStore(t), pem)
	for _, fragment := range []string{
		"MIIEpAIBAAKCAQEAx1yQGcXVpZXI0ISEh",
		"kQ2h5Y9eZv7tG6fJ3n2cB8xW4vLpM1sK9dR7uT0eY5oP3iF8aD6sH4gU",
		"-----END RSA PRIVATE KEY-----",
	} {
		if strings.Contains(payload, fragment) {
			t.Errorf("SECURITY: [secret-at-rest] private-key material survived redaction (redact.go covers PEM keys in name only): payload=%s", payload)
		}
	}
	// The store must still record that SOMETHING was redacted, so the
	// fix cannot be "stop matching PEM at all".
	if !strings.Contains(payload, "«redacted") {
		t.Errorf("no redaction marker at all in payload=%s", payload)
	}
}

// TestRedactedPayloadStaysDecodable pins the integrity side of the
// same persist surface: redaction runs on the serialized envelope, so
// a replacement that eats an odd number of JSON quote characters would
// corrupt events.payload and break every replay (resume-from-id,
// export, transcript tooling) for that session. Property: for every
// secret shape above, the stored payload remains valid JSON.
func TestRedactedPayloadStaysDecodable(t *testing.T) {
	shapes := []string{
		`OPENROUTER_API_KEY="sk-or-v1-0123456789abcdef0123456789abcdef"`, // not-a-secret: synthetic fixture for a redaction/injection probe
		`ZAI_API_KEY='4f8c2a1be9d7406fbc3a90fd11e2.aBcDeFgHiJkLmNoP'`,    // not-a-secret: synthetic fixture for a redaction/injection probe
		"Authorization: Bearer dGhpcyBpcyBhIHNlY3JldCB0b2tlbg== trailing",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEAx1yQ\n-----END RSA PRIVATE KEY-----",              // not-a-secret: synthetic fixture for a redaction/injection probe
		"id_token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N", // not-a-secret: synthetic unsigned JWT shape, exists only to prove redaction
	}
	for _, shape := range shapes {
		payload := appendTextDelta(t, newStore(t), shape)
		if !json.Valid([]byte(payload)) {
			t.Errorf("redaction corrupted the stored envelope; replay will fail to decode: %s", payload)
		}
	}
}
