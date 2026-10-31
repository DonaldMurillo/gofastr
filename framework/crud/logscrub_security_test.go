package crud

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A driver error quotes the request's own value back: Postgres answers
// an invalid uuid key with the text it was handed, ESC and newline
// included, so GET /legacy/%1B%5B2J%0AFORGED forged a log line through
// the raw %v in CrudHandler.Get.
func TestLogfScrubsControlBytes(t *testing.T) {
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) }()

	logf("crud: get query failed: %v", errors.New("invalid input syntax for type uuid: \"\x1b[2J\nFORGED\""))
	got := strings.TrimSuffix(buf.String(), "\n")
	if strings.ContainsAny(got, "\x1b\n\r") {
		t.Fatalf("log line kept control bytes: %q", got)
	}
	if !strings.Contains(got, "%1b[2J%0aFORGED") {
		t.Fatalf("log line = %q, want the controls percent-encoded", got)
	}
}

// Every crud log line goes through logf: the errors these sites print
// come from drivers and hooks, which can carry request bytes.
func TestCrudLogsOnlyThroughLogf(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	raw := regexp.MustCompile(`\blog\.(Printf|Println|Print)\(`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "logscrub.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if raw.MatchString(line) {
				t.Errorf("%s:%d logs raw; use logf: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
