package config_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

type escapedSecretCfg struct {
	Token string `config:"TOKEN" sensitive:"true"`
	Plain string `config:"PLAIN"`
	spell func(string) string
}

func (c *escapedSecretCfg) Validate() error {
	return errors.New("bad token " + c.spell(c.Token) + " (plain " + c.Plain + ")")
}

// Property: redaction scrubs the escaped spellings of a secret, not
// only its raw bytes. A validator writing fmt.Errorf("%q", tok) escapes
// a '"' or '\' in the token, so the raw value no longer matches and the
// whole credential rode into MustLoad's panic.
func TestRedactEscapedSecretSpellings(t *testing.T) {
	const secret = `s3cr"et\pässwörd/tail&x=1` // fixture, asserted REDACTED
	jsonSpell := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	spellings := map[string]func(string) string{
		"%q":         func(s string) string { return fmt.Sprintf("%q", s) },
		"%+q":        func(s string) string { return fmt.Sprintf("%+q", s) },
		"%#q":        func(s string) string { return fmt.Sprintf("%#q", s) },
		"%v nested":  func(s string) string { return fmt.Sprintf("%q", []string{s}) },
		"json":       jsonSpell,
		"query":      url.QueryEscape,
		"path":       url.PathEscape,
		"raw":        func(s string) string { return s },
		"%q of %q":   func(s string) string { return fmt.Sprintf("%q", fmt.Sprintf("%q", s)) },
		"json of %q": func(s string) string { return jsonSpell(fmt.Sprintf("%q", s)) },
	}
	for name, spell := range spellings {
		c := escapedSecretCfg{spell: spell}
		err := config.Load(&c, config.MapSource{"TOKEN": secret, "PLAIN": "diag"})
		if err == nil {
			t.Fatalf("%s: expected the validator error", name)
		}
		msg := err.Error()
		for _, frag := range []string{"s3cr", "tail", "ss"} {
			if strings.Contains(msg, frag) {
				t.Errorf("SECURITY: [config] %s spelling leaked secret fragment %q: %s", name, frag, msg)
			}
		}
		if !strings.Contains(msg, "(redacted)") || !strings.Contains(msg, "diag") {
			t.Errorf("[config] %s: lost the placeholder or the context: %s", name, msg)
		}
	}
}
