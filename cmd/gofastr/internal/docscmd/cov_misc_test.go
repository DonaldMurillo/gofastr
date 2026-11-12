package docscmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ── docs.go ───────────────────────────────────────────────────────────

func TestPrintDocsHelp(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { RunDocs([]string{"--help"}) })
	if !strings.Contains(out, "browse framework docs") {
		t.Fatalf("docs help: %s", out)
	}
}

func TestRunDocsUnknownTopicExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunDocs([]string{"no-such-topic-xyz"}) })
	})
	if code != 1 {
		t.Fatalf("want 1 got %d", code)
	}
}

func TestRunDocsGrepEmptyTermExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunDocs([]string{"--grep"}) })
	})
	if code != 2 {
		t.Fatalf("want 2 got %d", code)
	}
}

func TestRunDocsGrepNoMatch(t *testing.T) {
	out := clitest.CovT_capStdout(t, func() { RunDocs([]string{"--grep", "zzzznevermatchqqq"}) })
	if !strings.Contains(out, "No matches") {
		t.Fatalf("expected no matches: %s", out)
	}
}

func TestPluralSAndHighlight(t *testing.T) {
	if pluralS(1) != "" || pluralS(2) != "es" {
		t.Fatal("pluralS")
	}
	if highlight("hello world", "") != "hello world" {
		t.Fatal("highlight empty term")
	}
	got := highlight("Foo foo FOO", "foo")
	if !strings.Contains(got, "Foo") {
		t.Fatalf("highlight: %s", got)
	}
}

// TestRunDocsGrepRefusesFlagTerm: `--grep --serve` searched for the
// literal "--serve" and exited 0. A dash term still works as --grep=-x.
func TestRunDocsGrepRefusesFlagTerm(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunDocs([]string{"--grep", "--serve"}) })
	})
	if code != 2 {
		t.Fatalf("--grep --serve: want exit 2, got %d", code)
	}
	var out string
	code = clitest.CovT_capExit(t, func() {
		out = clitest.CovT_capStdout(t, func() { RunDocs([]string{"--grep=--serve"}) })
	})
	if code != -1 {
		t.Fatalf("--grep=--serve: want no exit, got %d", code)
	}
	if !strings.Contains(out, "--serve") {
		t.Fatalf("--grep=--serve must search: %.200s", out)
	}
}
