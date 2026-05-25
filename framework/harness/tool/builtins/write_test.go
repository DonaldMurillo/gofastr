package builtins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "x.txt")
	res, err := (Write{}).Run(context.Background(), mustCall(t, map[string]any{
		"path": path, "content": "hello",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want hello", string(got))
	}
}

func TestEditSingleOccurrence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(path, []byte("foo bar baz"), 0o600)
	res, _ := (Edit{}).Run(context.Background(), mustCall(t, map[string]any{
		"path": path, "old_string": "bar", "new_string": "QUX",
	}), nil)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "foo QUX baz" {
		t.Errorf("content = %q", string(got))
	}
}

func TestEditRefusesAmbiguous(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(path, []byte("a a a"), 0o600)
	res, _ := (Edit{}).Run(context.Background(), mustCall(t, map[string]any{
		"path": path, "old_string": "a", "new_string": "b",
	}), nil)
	if !res.IsError {
		t.Fatal("expected error for ambiguous old_string")
	}
	// Original file untouched.
	got, _ := os.ReadFile(path)
	if string(got) != "a a a" {
		t.Errorf("file modified despite ambiguous old_string: %q", string(got))
	}
}

func TestEditReplaceAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(path, []byte("a a a"), 0o600)
	res, _ := (Edit{}).Run(context.Background(), mustCall(t, map[string]any{
		"path": path, "old_string": "a", "new_string": "b", "replace_all": true,
	}), nil)
	if res.IsError {
		t.Fatalf("unexpected error: %+v", res)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "b b b" {
		t.Errorf("content = %q", string(got))
	}
}

func TestEditMissingOldString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.txt")
	_ = os.WriteFile(path, []byte("foo"), 0o600)
	res, _ := (Edit{}).Run(context.Background(), mustCall(t, map[string]any{
		"path": path, "old_string": "missing", "new_string": "x",
	}), nil)
	if !res.IsError {
		t.Fatal("expected error for missing old_string")
	}
}
