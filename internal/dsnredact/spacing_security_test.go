package dsnredact

import (
	"fmt"
	"strings"
	"testing"
)

func TestSpacedPasswordsAreSecret(t *testing.T) {
	for _, key := range []string{"password", "sslpassword"} {
		for _, assignment := range []string{"%s = fixture", "%s= fixture", "%s =fixture", "%s\t=\tfixture", "%s\n=\rfixture", "%s = 'fixture phrase'", "%s = 'fixture\\' phrase'", "%s = fixture\\ phrase"} {
			dsn := "host = db " + fmt.Sprintf(assignment, key) + " user = app dbname=shop"
			if !HasSecret(dsn) {
				t.Errorf("missed spaced credential: %q", dsn)
			}
			got := Redact(dsn)
			if strings.Contains(got, "fixture") || strings.Contains(got, "phrase") || strings.Contains(got, key) {
				t.Errorf("credential leaked: %q", got)
			}
			if !strings.Contains(got, "host = db") || !strings.Contains(got, "user = app") || !strings.Contains(got, "dbname=shop") {
				t.Errorf("lost configuration: %q", got)
			}
		}
		dsn := "host=db application_name=app's " + key + " = fixture dbname=shop"
		if !HasSecret(dsn) || strings.Contains(Redact(dsn), "fixture") {
			t.Errorf("unquoted apostrophe hid the credential: %q", dsn)
		}
	}
}

func TestQuotedOptionsAreNotPasswords(t *testing.T) {
	for _, dsn := range []string{"host = db user = app", "host=db options='password = fixture'", "host=db application_name='sslpassword=fixture'"} {
		if HasSecret(dsn) || Redact(dsn) != dsn {
			t.Errorf("password-free DSN changed: %q", dsn)
		}
	}
}
