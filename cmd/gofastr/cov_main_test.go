package main

import (
	"strings"
	"testing"
)

func TestDispatchNoArgsPrintsHelp(t *testing.T) {
	out := covT_capStdout(t, func() { dispatch(nil) })
	if !strings.Contains(out, "GoFastr CLI") {
		t.Fatalf("expected help, got: %s", out)
	}
}

func TestDispatchHelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		out := covT_capStdout(t, func() { dispatch([]string{flag}) })
		if !strings.Contains(out, "Commands") {
			t.Fatalf("%s: expected help, got %s", flag, out)
		}
	}
}

func TestCommandSpecificHelp(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
	}{
		{"init", "generated go.mod pins"},
		{"generate", "Generate a deterministic application"},
		{"validate", "validate a blueprint without writing files"},
	} {
		out := covT_capStdout(t, func() { dispatch([]string{tc.command, "--help"}) })
		if !strings.Contains(out, tc.want) {
			t.Errorf("%s --help missing %q:\n%s", tc.command, tc.want, out)
		}
		if strings.Contains(out, "Start dev server with auto-restart") {
			t.Errorf("%s --help fell back to global help:\n%s", tc.command, out)
		}
	}
}

func TestDispatchVersionFlag(t *testing.T) {
	for _, flag := range []string{"--version", "-v", "version"} {
		out := covT_capStdout(t, func() { dispatch([]string{flag}) })
		if !strings.Contains(out, "GoFastr") || !strings.Contains(out, "commit") {
			t.Fatalf("%s: expected version, got %s", flag, out)
		}
	}
}

func TestDispatchUnknownCmdExits(t *testing.T) {
	var out string
	code := covT_capExit(t, func() {
		out = covT_capStdout(t, func() { dispatch([]string{"qq"}) })
	})
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	_ = out
}

func TestDispatchUnknownCmdSuggests(t *testing.T) {
	out := covT_capStdout(t, func() {
		_ = covT_capExit(t, func() { dispatch([]string{"versio"}) })
	})
	if !strings.Contains(out, "Did you mean") {
		t.Fatalf("expected suggestion, got %s", out)
	}
}

func TestColorHelpersTTYBranch(t *testing.T) {
	covT_withTTY(func() {
		for _, got := range []string{green("x"), red("x"), yellow("x"), bold("x"), dim("x")} {
			if !strings.Contains(got, "\033[") {
				t.Fatalf("expected ANSI escape on TTY, got %q", got)
			}
		}
	})
}

func TestInfoWarnPrint(t *testing.T) {
	out := covT_capStdout(t, func() {
		info("hello %d", 1)
		warn("careful %s", "now")
	})
	if !strings.Contains(out, "hello 1") || !strings.Contains(out, "careful now") {
		t.Fatalf("unexpected info/warn output: %s", out)
	}
}

// Edit distance now lives in core/fuzzy (shared with framework/filter) and is
// tested there; here we only cover the CLI-local variadic min helper.
func TestMin(t *testing.T) {
	if min(3, 1, 2) != 1 {
		t.Fatalf("min broken")
	}
	if min(5) != 5 {
		t.Fatalf("min single-arg broken")
	}
}
