package ownstyle

import (
	"os"
	"strings"
	"testing"
)

// The board sheet documents its over-limit flag on the rule
// ".column.over-limit .count". That doc belongs above OverLimit, never
// above the Count method the descendant compound also yields.
func TestGenDocSkipsDescendantClass(t *testing.T) {
	src, err := os.ReadFile("testdata/board.style.css")
	if err != nil {
		t.Fatal(err)
	}
	m := modelFile(t, "board.style.css")
	out, err := GenerateFile("board", KindScoped, string(src), m, "board", false)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(out, ") Count(")
	if i < 0 {
		t.Fatalf("no Count method in:\n%s", out)
	}
	above := out[strings.LastIndex(out[:i], "\n}\n")+1 : i]
	if strings.Contains(above, "WIP limit") {
		t.Errorf("Count carries the over-limit doc:\n%s", above)
	}
	if !strings.Contains(out, "WIP limit") {
		t.Errorf("over-limit doc missing from the output")
	}
}
