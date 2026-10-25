package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestDispatchNoArgsPrintsHelp(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { dispatch(nil) })
	if !strings.Contains(out, "GoFastr CLI") {
		t.Fatalf("expected help, got: %s", out)
	}
}

func TestDispatchHelpFlag(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		out := clitest.CovT_capStdout(t, func() { dispatch([]string{flag}) })
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
		{"generate", "Scaffold generators write code you own"},
		{"validate", "validate an app spec without writing files"},
	} {
		out := clitest.CovT_capStdout(t, func() { dispatch([]string{tc.command, "--help"}) })
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
		out := clitest.CovT_capStdout(t, func() { dispatch([]string{flag}) })
		if !strings.Contains(out, "GoFastr") || !strings.Contains(out, "commit") {
			t.Fatalf("%s: expected version, got %s", flag, out)
		}
	}
}

func TestDispatchUnknownCmdExits(t *testing.T) {
	var out string
	code := clitest.CovT_capExit(t, func() {
		out = clitest.CovT_capStdout(t, func() { dispatch([]string{"qq"}) })
	})
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	_ = out
}

func TestDispatchUnknownCmdSuggests(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() {
		_ = clitest.CovT_capExit(t, func() { dispatch([]string{"versio"}) })
	})
	if !strings.Contains(out, "Did you mean") {
		t.Fatalf("expected suggestion, got %s", out)
	}
}

func TestColorHelpersTTYBranch(t *testing.T) {
	clitest.CovT_withTTY(func() {
		for _, got := range []string{cli.Green("x"), cli.Red("x"), cli.Yellow("x"), cli.Bold("x"), cli.Dim("x")} {
			if !strings.Contains(got, "\033[") {
				t.Fatalf("expected ANSI escape on TTY, got %q", got)
			}
		}
	})
}

func TestInfoWarnPrint(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() {
		cli.Info("hello %d", 1)
		cli.Warn("careful %s", "now")
	})
	if !strings.Contains(out, "hello 1") || !strings.Contains(out, "careful now") {
		t.Fatalf("unexpected info/warn output: %s", out)
	}
}
