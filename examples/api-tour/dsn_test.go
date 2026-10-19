package main

import "testing"

func TestDatabaseURLPrefersTheEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "file:/elsewhere/api-tour.db")
	if got := databaseURL(); got != "file:/elsewhere/api-tour.db" {
		t.Fatalf("databaseURL() = %q, want DATABASE_URL's value", got)
	}
	t.Setenv("DATABASE_URL", "")
	if got := databaseURL(); got != "./api-tour.db" {
		t.Fatalf("databaseURL() = %q, want the ./api-tour.db default", got)
	}
}
