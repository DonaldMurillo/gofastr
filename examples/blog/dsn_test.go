package main

import "testing"

func TestDatabaseURLPrefersTheEnv(t *testing.T) {
	t.Setenv("DATABASE_URL", "file:/elsewhere/blog.db")
	if got := databaseURL(); got != "file:/elsewhere/blog.db" {
		t.Fatalf("databaseURL() = %q, want DATABASE_URL's value", got)
	}
	t.Setenv("DATABASE_URL", "")
	if got := databaseURL(); got != "./blog.db" {
		t.Fatalf("databaseURL() = %q, want the ./blog.db default", got)
	}
}
