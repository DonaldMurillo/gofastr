package i18nui

import (
	"strings"
	"testing"
)

func TestTWithNilReturnsDefault(t *testing.T) {
	for key, expected := range Defaults {
		got := TWith(nil, key)
		if got != expected {
			t.Errorf("TWith(nil, %q) = %q, want %q", key, got, expected)
		}
	}
}

func TestTReturnsDefault(t *testing.T) {
	got := T(KeyPaginationNext)
	if got != "Next" {
		t.Fatalf("T(KeyPaginationNext) = %q, want %q", got, "Next")
	}
}

func TestTUnknownKeyReturnsKeyString(t *testing.T) {
	key := Key("ui.nonexistent.key")
	got := T(key)
	if got != string(key) {
		t.Fatalf("T(%q) = %q, want %q", key, got, key)
	}
}

func TestValidationError(t *testing.T) {
	got := ValidationError("required", nil)
	if got != "This field is required" {
		t.Fatalf("ValidationError(required) = %q", got)
	}
}

func TestValidationErrorWithVars(t *testing.T) {
	got := ValidationError("min", map[string]string{"min": "5"})
	if got != "Must be at least 5" {
		t.Fatalf("ValidationError(min, {min:5}) = %q", got)
	}
}

func TestValidationErrorMinLength(t *testing.T) {
	got := ValidationError("minLength", map[string]string{"min": "3"})
	if !strings.Contains(got, "3") {
		t.Fatalf("expected '3' in %q", got)
	}
	if !strings.Contains(got, "characters") {
		t.Fatalf("expected 'characters' in %q", got)
	}
}

func TestLabelForFieldNoTranslator(t *testing.T) {
	tests := []struct {
		field, want string
	}{
		{"email", "Email"},
		{"first_name", "First Name"},
		{"createdAt", "Created At"},
		{"user-id", "User Id"},
	}
	for _, tt := range tests {
		got := LabelForField(nil, "user", tt.field)
		if got != tt.want {
			t.Errorf("LabelForField(nil, %q, %q) = %q, want %q", "user", tt.field, got, tt.want)
		}
	}
}

func TestLabelForFieldSnakeCase(t *testing.T) {
	got := LabelForField(nil, "order", "shipping_address_line_1")
	if !strings.Contains(got, "Shipping") {
		t.Fatalf("expected humanized label for shipping_address_line_1, got %q", got)
	}
}

func TestAllKeysHaveDefaults(t *testing.T) {
	// Enumerate all key constants and ensure each has a default.
	keys := []Key{
		KeyPaginationPrevious, KeyPaginationNext, KeyPaginationPage,
		KeyPaginationOf, KeyPaginationShowing, KeyPaginationResults,
		KeyValidationRequired, KeyValidationEmail, KeyValidationMin,
		KeyValidationMax, KeyValidationMinLen, KeyValidationMaxLen,
		KeyValidationPattern, KeyValidationUnique,
		KeyEmptyStateTitle, KeyEmptyStateDesc,
		KeyDialogConfirm, KeyDialogCancel, KeyDialogClose,
		KeyDialogSave, KeyDialogDelete,
		KeyToastSuccess, KeyToastError, KeyToastWarning, KeyToastInfo,
		KeyBannerDismiss,
		KeyTableSortAsc, KeyTableSortDesc, KeyTableNoSort,
		KeyTableFilter, KeyTableNoResults, KeyTableLoading,
		KeyFileUploadDrop, KeyFileUploadBrowse, KeyFileUploadRemove,
		KeyFormSubmit, KeyFormReset, KeyFormSending,
		KeyFormSuccess, KeyFormError, KeyFormYes, KeyFormNo,
		KeySearchPlaceholder, KeySearchNoResults,
		KeyAuthLogin, KeyAuthLogout, KeyAuthSignup,
		KeyAuthEmail, KeyAuthPassword, KeyAuthRememberMe,
	}
	for _, k := range keys {
		if _, ok := Defaults[k]; !ok {
			t.Errorf("key %q has no default string", k)
		}
	}
}

func TestHumanize(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"email", "Email"},
		{"first_name", "First Name"},
		{"created_at", "Created At"},
		{"id", "Id"},
		{"a", "A"},
	}
	for _, tt := range tests {
		got := humanize(tt.input)
		if got != tt.want {
			t.Errorf("humanize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
