package ui

import (
	"strings"
	"testing"
)

func TestSummaryEmpty(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{}))
	if got != "" {
		t.Errorf("expected empty render for no errors, got: %s", got)
	}
}

func TestListsEachError(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{
			"email": "must be a valid email",
			"name":  "is required",
		},
	}))
	if !strings.Contains(got, "must be a valid email") {
		t.Errorf("missing email error, got: %s", got)
	}
	if !strings.Contains(got, "is required") {
		t.Errorf("missing name error, got: %s", got)
	}
}

func TestAnchorLinksFieldName(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"email": "bad"},
	}))
	if !strings.Contains(got, `href="#email"`) {
		t.Errorf("expected anchor href=\"#email\", got: %s", got)
	}
}

func TestFieldIDsOverrideAnchor(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors:   FieldErrors{"email": "bad"},
		FieldIDs: map[string]string{"email": "form-email-input"},
	}))
	if !strings.Contains(got, `href="#form-email-input"`) {
		t.Errorf("expected mapped FieldID in anchor, got: %s", got)
	}
}

func TestUsesFieldLabel(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors:      FieldErrors{"email": "bad"},
		FieldLabels: map[string]string{"email": "Email address"},
	}))
	if !strings.Contains(got, "Email address") {
		t.Errorf("expected label 'Email address', got: %s", got)
	}
}

func TestRoleAlertAndAriaLive(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"x": "y"},
	}))
	if !strings.Contains(got, `role="alert"`) {
		t.Errorf("expected role=\"alert\", got: %s", got)
	}
	if !strings.Contains(got, `aria-live="assertive"`) {
		t.Errorf("expected aria-live=\"assertive\", got: %s", got)
	}
}

func TestFieldOrderControlsRowOrder(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{
			"email": "bad",
			"name":  "required",
			"age":   "must be a number",
		},
		FieldOrder: []string{"name", "email", "age"},
	}))
	nameIdx := strings.Index(got, "required")
	emailIdx := strings.Index(got, "bad")
	ageIdx := strings.Index(got, "must be a number")
	if !(nameIdx < emailIdx && emailIdx < ageIdx) {
		t.Errorf("expected order name→email→age, got name=%d email=%d age=%d\n%s",
			nameIdx, emailIdx, ageIdx, got)
	}
}

func TestFieldOrderSkipsMissing(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors:     FieldErrors{"email": "bad"},
		FieldOrder: []string{"name", "email", "age"}, // only email has an error
	}))
	if c := strings.Count(got, `href="#`); c != 1 {
		t.Errorf("expected exactly 1 anchor (only email has an error), got %d\n%s", c, got)
	}
}

func TestAlphabeticalFallback(t *testing.T) {
	// Without FieldOrder, leftover keys should sort alphabetically so
	// output is deterministic across requests (Go map iteration is
	// randomized otherwise).
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{
			"zip":   "bad zip",
			"alpha": "bad alpha",
			"name":  "bad name",
		},
	}))
	a := strings.Index(got, "bad alpha")
	n := strings.Index(got, "bad name")
	z := strings.Index(got, "bad zip")
	if !(a < n && n < z) {
		t.Errorf("expected alphabetical order alpha→name→zip, got positions a=%d n=%d z=%d\n%s",
			a, n, z, got)
	}
}

func TestCustomTitle(t *testing.T) {
	got := string(ValidationSummary(ValidationSummaryConfig{
		Errors: FieldErrors{"x": "y"},
		Title:  "Fix these problems",
	}))
	if !strings.Contains(got, "Fix these problems") {
		t.Errorf("expected custom title, got: %s", got)
	}
}
