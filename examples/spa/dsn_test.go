package main

import "testing"

func TestDatabaseURLPrefersTheEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "file:/elsewhere/spa.db")
	if got := databaseURL(); got != "file:/elsewhere/spa.db" {
		t.Fatalf("databaseURL() = %q, want DATABASE_URL's value", got)
	}
	t.Setenv("DATABASE_URL", "")
	if got := databaseURL(); got != "./spa.db" {
		t.Fatalf("databaseURL() = %q, want the ./spa.db default", got)
	}
}
