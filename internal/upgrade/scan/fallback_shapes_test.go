package scan

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// A shapes symbol survives its release, so a pre-patch app's call still
// resolves against the new-shaped kit and the regex written for the old
// shape does not match. The compile error on that line names the
// symbol, so the note gets the hit the typed walk withheld, carrying the
// error, and the error is explained.
func TestFallbackShapeCallExplained(t *testing.T) {
	kit := map[string]string{"ui/ui.go": "package ui\n\ntype Layout struct{}\n\nfunc (l *Layout) WithHeader(h, sub string) {}\n"}
	src := `package main

import "example.com/kit/ui"

func main() {
	var l ui.Layout
	l.WithHeader("h")
}
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: withHeaderSym, Type: regexp.MustCompile(`^func\(h string\)$`)},
	}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, `WithHeader("h")`, "main.go", withHeaderSym.String()+" shape"))
	if got := res.Hits[n][0].Err; !strings.Contains(got, "not enough arguments") {
		t.Fatalf("Err = %q, want the compile error", got)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

// A changed field type names neither the struct nor the field; the
// error is about the struct literal the shapes field sits in.
func TestFallbackShapeFieldTypeExplained(t *testing.T) {
	kit := map[string]string{"ui/ui.go": "package ui\n\ntype FormFieldConfig struct{ Input func() string }\n"}
	src := `package main

import "example.com/kit/ui"

func main() { _ = ui.FormFieldConfig{Input: "oops"} }
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	sym := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormFieldConfig", Member: "Input"}
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: sym, Type: regexp.MustCompile(`^string$`)},
	}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, `Input: "oops"`, "main.go", sym.String()+" shape"))
	if got := res.Hits[n][0].Err; !strings.Contains(got, "in struct literal") {
		t.Fatalf("Err = %q, want the struct literal error", got)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

// A loose regex that also matches the new shape yields a typed hit
// against the new-shaped kit; the compile error on its line is explained
// by that hit because the error names the symbol.
func TestFallbackShapeHitExplainsLine(t *testing.T) {
	kit := map[string]string{"ui/ui.go": "package ui\n\ntype Layout struct{}\n\nfunc (l *Layout) WithHeader(h, sub string) {}\n"}
	src := `package main

import "example.com/kit/ui"

func main() {
	var l ui.Layout
	l.WithHeader("h")
}
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: withHeaderSym, Type: regexp.MustCompile(`^func\(`)},
	}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n, hitAt(src, `WithHeader("h")`, "main.go", withHeaderSym.String()+" shape func(h string, sub string)"))
	if got := res.Hits[n][0].Err; got != "" {
		t.Fatalf("Err = %q, want empty: the typed matcher resolved the call", got)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}

// An error on a shapes site that names something else is not the
// shape change: it stays unexplained and the note gets no hit.
func TestFallbackShapeUnrelatedErrorKept(t *testing.T) {
	kit := map[string]string{"ui/ui.go": "package ui\n\ntype Layout struct{}\n\nfunc (l *Layout) WithHeader(h, sub string) {}\n"}
	src := `package main

import "example.com/kit/ui"

func main() {
	var l ui.Layout
	l.WithHeader("h", helper())
}
`
	root := newWorkspace(t, kit, map[string]string{"main.go": src})
	testEnv(t)
	n := &upgrade.Note{Find: upgrade.Find{Shapes: []upgrade.ShapeMatch{
		{Symbol: withHeaderSym, Type: regexp.MustCompile(`^func\(h string\)$`)},
	}}}
	res, err := Run(root, []*upgrade.Note{n}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, n)
	if len(res.Unexplained) != 1 || !strings.Contains(res.Unexplained[0].Why, "helper") {
		t.Fatalf("Unexplained = %v, want the undefined helper", hitStrs(res.Unexplained))
	}
}

// The implementation kept the old shape while the kit's interface moved
// on, so the compile error lands on an interface assertion: a line with
// no shapes site that spells neither the method nor its parameter types.
// The error names the interface and the method, which mints the hit the
// way an error naming a uses symbol does. The implementation's own
// declaration is not a hit here: against the new kit the type no longer
// implements the interface, so the defs walk does not reach it.
func TestFallbackShapeAssertionExplained(t *testing.T) {
	src := `package main

import (
	"context"

	"example.com/kit/queue"
)

type mem struct{}

func (m *mem) Ack(ctx context.Context, jobID string) error { return nil }

var _ queue.Queue = (*mem)(nil)

func main() {}
`
	root := newWorkspace(t, shapeQueueNewKit, map[string]string{"main.go": src})
	testEnv(t)
	res, err := Run(root, []*upgrade.Note{shapeAckNote}, upgrade.MarkerSinks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantHits(t, res, shapeAckNote, hitAt(src, "(*mem)(nil)", "main.go", shapeAckSym.String()+" shape"))
	if got := res.Hits[shapeAckNote][0].Err; !strings.Contains(got, "wrong type for method Ack") {
		t.Fatalf("Err = %q, want the assertion error", got)
	}
	if len(res.Unexplained) != 0 {
		t.Fatalf("Unexplained = %v, want none", hitStrs(res.Unexplained))
	}
}
