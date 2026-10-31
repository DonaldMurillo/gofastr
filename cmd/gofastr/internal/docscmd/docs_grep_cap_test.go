package docscmd

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// grepHitLine matches the line number a hit's heading and excerpt lines
// both start with, ANSI colour included.
var grepHitLine = regexp.MustCompile(`^  (?:\x1b\[[0-9;]*m)?L(\d+)`)

// countGrepHits counts the hits in --grep output: a hit prints one or two
// lines (its heading, then its excerpt) under its topic, all with its
// line number, so a hit is one distinct (topic, line) pair.
func countGrepHits(out string) int {
	type hit struct{ topic, line string }
	seen := map[hit]bool{}
	topic := ""
	for ln := range strings.SplitSeq(out, "\n") {
		if strings.Contains(ln, "─") {
			topic = ln
			continue
		}
		if m := grepHitLine.FindStringSubmatch(ln); m != nil {
			seen[hit{topic, m[1]}] = true
		}
	}
	return len(seen)
}

// A term with more hits than --grep prints says the list was cut,
// instead of printing the cap as the match total.
func TestDocsGrepSaysWhenCut(t *testing.T) {
	out := clitest.CaptureStdout(t, func() { RunDocs([]string{"--grep", "the"}) })
	if !strings.Contains(out, `More than 50 matches for "the"`) {
		t.Fatalf("a cut list must say so:\n%.400s", out)
	}
	if n := countGrepHits(out); n != grepHitCap {
		t.Fatalf("printed %d hits, want the cap, %d", n, grepHitCap)
	}
}

// A term with fewer hits than the cap prints its exact count.
func TestDocsGrepCountsAnUncutList(t *testing.T) {
	out := clitest.CaptureStdout(t, func() { RunDocs([]string{"--grep", "owner scoping"}) })
	n := countGrepHits(out)
	if n == 0 || n >= grepHitCap {
		t.Fatalf("precondition: want a term with 1..%d hits, got %d", grepHitCap-1, n)
	}
	if strings.Contains(out, "More than") {
		t.Fatalf("an uncut list said it was cut:\n%s", out)
	}
	if !strings.HasPrefix(out, strconv.Itoa(n)+" match") {
		t.Fatalf("the header does not state the %d hits printed:\n%.300s", n, out)
	}
}

// A term under three characters is refused, not answered "No matches".
func TestDocsGrepShortTermExits(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunDocs([]string{"--grep", "ab"}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
}
