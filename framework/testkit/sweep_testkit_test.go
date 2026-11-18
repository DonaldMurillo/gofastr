package testkit

import (
	"regexp"
	"strings"
	"testing"
)

func TestSweepUniqueDBNameIsAShortLowercaseIdentifier(t *testing.T) {
	ident := regexp.MustCompile(`^ftest_[a-z0-9_]+_[0-9a-f]{12}$`)
	t.Run("A_Very-Long/Mixed.Case Test Name That Goes On And On And On", func(t *testing.T) {
		a, b := newUniqueDBName(t), newUniqueDBName(t)
		if !ident.MatchString(a) || len(a) > 63 {
			t.Errorf("name %q is not a lowercase identifier of at most 63 bytes", a)
		}
		if a == b {
			t.Errorf("two names for one test are equal: %q", a)
		}
	})
}

func TestSweepRedactHidesThePassword(t *testing.T) {
	for _, dsn := range []string{"mysql://admin:s3cret@db/x", "postgres://admin:s3cret@db/x\x7f"} {
		if _, err := rewriteDBName(dsn, "ftest_x"); err == nil || strings.Contains(err.Error(), "s3cret") {
			t.Errorf("rewriteDBName(%q) error %v leaks the password or accepts the DSN", dsn, err)
		}
	}
	for _, dsn := range []string{"mysql://admin@db/x?password=q3cret&sslmode=disable", "mysql://admin@db/x?password=q3cret;sslmode=disable", "mysql:/\\x%zz?password=q3cret"} {
		if err := ValidateAdminDSN(dsn); err == nil || strings.Contains(err.Error(), "q3cret") {
			t.Errorf("ValidateAdminDSN(%q) error %v leaks a query-string password or accepts a non-postgres DSN", dsn, err)
		}
	}
	if err := ValidateAdminDSN("mysql://admin:s3cret@db/x"); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("ValidateAdminDSN error %v leaks the password or accepts a non-postgres DSN", err)
	}
}
