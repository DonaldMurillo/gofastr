package dsnredact

import (
	"net/url"
	"strings"
	"testing"
)

func TestRedactMalformedURLPasswordDelimiters(t *testing.T) {
	for _, test := range []struct {
		name       string
		credential string
	}{
		{name: "question mark", credential: "p?ss%zz"},
		{name: "fragment", credential: "pwhead#pwtail%zz"},
		{name: "slash", credential: "pwhead/pwtail%zz"},
		{name: "multiple at signs", credential: "pwhead@pwtail@%zz"},
		{name: "mixed delimiters", credential: "pwhead?middle#more/end@pwtail%zz"},
		{name: "delimiter after at sign", credential: "pwhead@middle/pwtail%zz"},
		{name: "bad escape", credential: "pwhead%zzpwtail"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dsn := "postgres://u:" + test.credential + "@host/db?password=querysecret&sslpassword=tlssecret"
			if _, err := url.Parse(dsn); err == nil {
				t.Fatal("fixture must exercise the textual fallback")
			}
			if !HasSecret(dsn) {
				t.Fatal("malformed userinfo was classified as public")
			}
			got := Redact(dsn)
			for _, secret := range []string{test.credential, "u:p", "pwhead", "pwtail", "ss%zz", "querysecret", "tlssecret"} {
				if strings.Contains(got, secret) {
					t.Errorf("credential fragment %q survived: %q", secret, got)
				}
			}
			if again := Redact(got); again != got {
				t.Errorf("redaction is not idempotent: %q -> %q", got, again)
			}
		})
	}
}

func TestRedactMalformedURLQueryWithoutUserinfo(t *testing.T) {
	dsn := "postgres://host:%zz/db?password=querysecret&sslpassword=tlssecret"
	if !HasSecret(dsn) {
		t.Error("malformed URL query credentials were classified as public")
	}
	if got := Redact(dsn); got != "postgres://host:%zz/db" {
		t.Errorf("Redact() = %q, want malformed query removed", got)
	}
}

func TestRedactMalformedURLAmbiguousAtSigns(t *testing.T) {
	for _, suffix := range []string{"/db/querysecret@tailsecret", "?password=querysecret@tailsecret", "#querysecret@tailsecret"} {
		dsn := "postgres://u:pwhead%zz@host" + suffix
		got := Redact(dsn)
		for _, secret := range []string{"pwhead", "querysecret", "tailsecret"} {
			if strings.Contains(got, secret) {
				t.Errorf("ambiguous malformed URL leaked %q: %q", secret, got)
			}
		}
	}
}
