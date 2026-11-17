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
	got := redact("postgres://admin:s3cret@db:5432/postgres")
	if strings.Contains(got, "s3cret") || !strings.Contains(got, "admin") {
		t.Errorf("redact = %q, want the user kept and the password hidden", got)
	}
	if err := ValidateAdminDSN("mysql://admin:s3cret@db/x"); err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("ValidateAdminDSN error %v leaks the password or accepts a non-postgres DSN", err)
	}
}
