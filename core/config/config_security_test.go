package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// Property: a field tagged `required:"true"` is satisfied ONLY by a
// non-empty value. Surfaces: key absent, key present-but-empty, and an
// empty `default:` tag.
//
// EnvSource deliberately distinguishes unset from set-but-empty
// (EnvSource.Get returns ("", true) for `SECRET=`), which is what made
// present-but-empty skip the !found guard entirely and bind the zero
// value. `SECRET=` in a .env file, a k8s ConfigMap key with no value, or
// a secret-manager miss that writes an empty string all take that path,
// so a signing key or DB URL silently downgrades to "" instead of
// refusing to boot.
func TestRequiredRejectsEmptyValue(t *testing.T) {
	type cfg struct {
		Secret string `config:"SECRET" required:"true"`
	}
	cases := []struct {
		name string
		src  config.MapSource
	}{
		{"absent", config.MapSource{}},
		{"set-but-empty", config.MapSource{"SECRET": ""}},
	}
	for _, tc := range cases {
		var c cfg
		err := config.Load(&c, tc.src)
		if err == nil {
			t.Errorf("SECURITY: [config] required field satisfied by %s (cfg=%+v). "+
				"Attack: an empty signing key / DB URL silently binds the zero value "+
				"instead of refusing to boot.", tc.name, c)
			continue
		}
		if !strings.Contains(err.Error(), "SECRET") {
			t.Errorf("[config] %s error %q does not name the key", tc.name, err)
		}
	}

	// A required field WITH a default still binds the default when the
	// key is absent; the escape hatch stays open.
	type withDefault struct {
		Port string `config:"PORT" required:"true" default:"8080"`
	}
	var d withDefault
	if err := config.Load(&d, config.MapSource{}); err != nil || d.Port != "8080" {
		t.Errorf("[config] required+default regressed: err=%v port=%q", err, d.Port)
	}
}

type sensitiveCfg struct {
	DBURL  string `config:"DB_URL" sensitive:"true"`
	Nested struct {
		Token string `config:"TOKEN" sensitive:"true"`
	}
	Public string `config:"PUBLIC"`
}

func (c *sensitiveCfg) Validate() error {
	return fmt.Errorf("bad DSN %q / token %q / public %q", c.DBURL, c.Nested.Token, c.Public)
}

// Property: a value bound into a field tagged `sensitive:"true"` never
// appears in ANY error Load returns. Surfaces: the setField parse error
// (already pinned by TestSensitiveValueRedacted), the ConfigValidator
// hook's error, and a sensitive field inside a nested struct.
//
// Redaction used to wrap only setField's error; the validator's error
// was wrapped verbatim as "config: validate: %w", so a host writing the
// idiomatic fmt.Errorf("bad DSN %q", c.DBURL) leaked the credential
// through MustLoad's panic and into crash logs.
func TestSensitiveRedactedInValidator(t *testing.T) {
	const secret = "postgres://u:hunter2@h/db"
	const token = "tok_live_abcdef" // not-a-secret: fixture value the test asserts is REDACTED from error text
	var c sensitiveCfg
	err := config.Load(&c, config.MapSource{
		"DB_URL":       secret,
		"NESTED_TOKEN": token,
		"PUBLIC":       "not-a-secret",
	})
	if err == nil {
		t.Fatal("expected the validator error")
	}
	for _, leaked := range []string{secret, token, "hunter2"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("SECURITY: [config] validator error leaked sensitive value %q: %s. "+
				"Attack: MustLoad panics the credential into crash logs / stderr.", leaked, err)
		}
	}
	// Non-sensitive context must survive so the error stays diagnosable.
	if !strings.Contains(err.Error(), "not-a-secret") {
		t.Errorf("[config] redaction scrubbed non-sensitive context too: %s", err)
	}
}

type nestedSecretCfg struct {
	DSN   string `config:"DSN" sensitive:"true"`
	Token string `config:"TOKEN" sensitive:"true"`
	Plain string `config:"PLAIN"`
}

func (c *nestedSecretCfg) Validate() error {
	return fmt.Errorf("conn %q with bearer %q (plain %q)", c.DSN, c.Token, c.Plain)
}

// Property: redaction is ordered longest-secret-first, so a sensitive value
// that CONTAINS another sensitive value cannot leave a fragment behind.
//
// A DSN embeds its password ("postgres://svc:hunter2@..."), and hosts
// routinely mark both the DSN and a separately-configured token sensitive.
// Scrubbing in map/field order would replace the short token inside the
// DSN first, leaving "postgres://svc:(redacted)2@..." — a partial
// credential in the log. The longest-first sort is what makes the pair
// safe regardless of field order.
func TestRedactLongestSecretFirst(t *testing.T) {
	const (
		dsn   = "postgres://svc:hunter2@db.internal:5432/prod?sslmode=require"
		token = "hunter2" // the password, also configured as TOKEN — fixture, asserted REDACTED
	)
	var c nestedSecretCfg
	err := config.Load(&c, config.MapSource{
		"DSN":   dsn,
		"TOKEN": token,
		"PLAIN": "diagnostic-context",
	})
	if err == nil {
		t.Fatal("expected the validator error")
	}
	msg := err.Error()
	for _, leaked := range []string{dsn, token, "5432"} {
		if strings.Contains(msg, leaked) {
			t.Errorf("SECURITY: [config] error leaked %q despite longest-first redaction: %s. "+
				"Attack: a fragment of the DSN (password, host, port) rides into crash logs.", leaked, msg)
		}
	}
	if !strings.Contains(msg, "(redacted)") || !strings.Contains(msg, "diagnostic-context") {
		t.Errorf("[config] redaction lost the placeholder or the non-sensitive context: %s", msg)
	}
}
