package query

import (
	"testing"
)

func TestSafeIdent_Valid(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"users"},
		{"auth_users"},
		{"_private"},
		{"schema1.table2"},
		{"a"},
		{"A1"},
	}
	for _, tt := range tests {
		got, err := SafeIdent(tt.input)
		if err != nil {
			t.Errorf("SafeIdent(%q): unexpected error: %v", tt.input, err)
		}
		if got != tt.input {
			t.Errorf("SafeIdent(%q) = %q, want %q", tt.input, got, tt.input)
		}
	}
}

func TestSafeIdent_Invalid(t *testing.T) {
	tests := []struct {
		input string
	}{
		{""},                          // empty
		{"users; DROP TABLE users"},   // injection
		{"users;--"},                  // comment injection
		{"1table"},                    // starts with digit
		{"user name"},                 // space
		{"user'name"},                 // single quote
		{`user"name`},                 // double quote
		{"user`name"},                 // backtick
		{"user\tname"},                // tab
		{"user\nname"},                // newline
		{"DROP TABLE users"},          // SQL keyword with space
		{"users WHERE 1=1"},           // WHERE injection
		{".dotstart"},                 // starts with dot
		{"users."},                    // trailing dot
		{"schema..table"},             // double dot
		{"schema.1bad"},               // after dot starts with digit
	}
	for _, tt := range tests {
		_, err := SafeIdent(tt.input)
		if err == nil {
			t.Errorf("SafeIdent(%q): expected error, got nil", tt.input)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"users", `"users"`},
		{`weird"name`, `"weird""name"`},
		{"_private", `"_private"`},
	}
	for _, tt := range tests {
		got := QuoteIdent(tt.input)
		if got != tt.want {
			t.Errorf("QuoteIdent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSafeQuote(t *testing.T) {
	got, err := SafeQuote("users")
	if err != nil {
		t.Fatalf("SafeQuote: %v", err)
	}
	if got != `"users"` {
		t.Errorf("SafeQuote(users) = %q, want %q", got, `"users"`)
	}

	_, err = SafeQuote("users; DROP TABLE users")
	if err == nil {
		t.Error("SafeQuote(injection): expected error")
	}
}

func TestMustIdent(t *testing.T) {
	got := MustIdent("users")
	if got != "users" {
		t.Errorf("MustIdent = %q, want %q", got, "users")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("MustIdent(injection): expected panic")
		}
	}()
	MustIdent("users; DROP TABLE users")
}
