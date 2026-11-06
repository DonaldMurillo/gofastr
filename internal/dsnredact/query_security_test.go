package dsnredact

import (
	"strings"
	"testing"
)

func TestRedactAllPasswordLocations(t *testing.T) {
	for _, dsn := range []string{
		"postgres://db:5432/app?password=fixture&sslmode=require",
		"postgres://user:fixture@db/app?password=fixture&sslpassword=fixture&sslmode=require",
		"postgres://db/app?%70assword=fixture&sslpassword=fixture&sslmode=require",
		"postgres://db/app?password=fixture&password=fixture&sslmode=require",
		"postgres://user:bad%zz@db/app?password=fixture&sslpassword=fixture",
		"postgres://db/app?password=fixture%zz",
		"host=db user=app sslpassword=fixture dbname=app",
		"host=db password=fixture sslpassword=fixture dbname=app",
	} {
		t.Run(dsn, func(t *testing.T) {
			if !HasSecret(dsn) {
				t.Error("password-bearing DSN classified as public")
			}
			got := Redact(dsn)
			if strings.Contains(got, "fixture") || strings.Contains(got, "bad%zz") {
				t.Fatalf("credential leaked: %s", got)
			}
			if !strings.Contains(got, "db") {
				t.Errorf("lost non-secret host: %s", got)
			}
			if strings.Contains(dsn, "sslmode=require") && !strings.Contains(got, "sslmode=require") {
				t.Errorf("lost non-secret query setting: %s", got)
			}
			if again := Redact(got); again != got {
				t.Errorf("redaction is not idempotent: %s -> %s", got, again)
			}
		})
	}
}
