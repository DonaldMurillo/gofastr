package semantic

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

func TestPollIntervalDocMatchesDefaults(t *testing.T) {
	source, err := parser.ParseFile(token.NewFileSet(), "watcher.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var comment string
	ast.Inspect(source, func(node ast.Node) bool {
		field, ok := node.(*ast.Field)
		if ok && len(field.Names) == 1 && field.Names[0].Name == "PollInterval" {
			comment = field.Doc.Text()
		}
		return true
	})
	if strings.Contains(comment, "<= 0") || !strings.Contains(comment, "negative") || !strings.Contains(comment, "0") || !strings.Contains(comment, "2 seconds") {
		t.Fatalf("PollInterval documentation must distinguish zero-default from negative-disabled:\n%s", comment)
	}
	for _, interval := range []time.Duration{0, -1, -time.Second, 37 * time.Millisecond} {
		want := interval
		if want == 0 {
			want = 2 * time.Second
		}
		if got := NewWatcher(nil, WatchOptions{PollInterval: interval}).opts.PollInterval; got != want {
			t.Fatalf("PollInterval %s normalized to %s, want %s", interval, got, want)
		}
	}
}
