package main

import (
	"strings"
	"testing"
)

func TestBuildRejectsUnknownFlag(t *testing.T) {
	_, err := parseBuildOptions([]string{"--pgk", "./cmd/example"})
	if err == nil {
		t.Fatal("expected mistyped build flag to fail")
	}
	if !strings.Contains(err.Error(), "--pgk") {
		t.Fatalf("error %q does not identify unknown flag", err)
	}
}

func TestBuildRejectsUnexpectedPositionalArgument(t *testing.T) {
	_, err := parseBuildOptions([]string{"./cmd/example"})
	if err == nil {
		t.Fatal("expected positional build argument to fail")
	}
	if !strings.Contains(err.Error(), "./cmd/example") {
		t.Fatalf("error %q does not identify unexpected argument", err)
	}
}

func TestBuildRejectsOptionLikePackageValue(t *testing.T) {
	_, err := parseBuildOptions([]string{"--pkg=-toolexec=helper"})
	if err == nil {
		t.Fatal("expected option-like package value to fail")
	}
	if !strings.Contains(err.Error(), "--pkg") {
		t.Fatalf("error %q does not identify --pkg", err)
	}
}
