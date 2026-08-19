package guardmut

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

const sample = `package p

func f(a, b int, err error) int {
	if a > 0 && b > 0 {
		return 1
	}
	if err != nil {
		return 2
	}
	if x := a + b; x > 3 || a == 1 {
		return 3
	}
	return 0
}
`

func TestFindCollectsBothDirections(t *testing.T) {
	got, err := Find("p.go", []byte(sample), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// Three ifs, two directions each.
	if len(got) != 6 {
		t.Fatalf("found %d guards, want 6: %v", len(got), got)
	}
	kinds := map[Kind]int{}
	for _, g := range got {
		kinds[g.Kind]++
	}
	if kinds[Never] != 3 || kinds[Always] != 3 {
		t.Errorf("kind split = %v, want 3 each", kinds)
	}
	if got[0].Cond != "a > 0 && b > 0" {
		t.Errorf("first condition = %q, want the full expression", got[0].Cond)
	}
}

func TestSkipErrNilDropsOnlyErrorPlumbing(t *testing.T) {
	got, err := Find("p.go", []byte(sample), Options{SkipErrNil: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("found %d guards with SkipErrNil, want 4: %v", len(got), got)
	}
	for _, g := range got {
		if strings.Contains(g.Cond, "err") {
			t.Errorf("SkipErrNil kept an error guard: %s", g)
		}
	}
}

// The whole point of the annotate-don't-replace design: a mutated file must
// still compile, or the run reports "no test failed" for a reason that has
// nothing to do with the tests. Every identifier stays referenced.
func TestMutantsStillParseAndKeepEveryIdentifier(t *testing.T) {
	guards, err := Find("p.go", []byte(sample), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range guards {
		out, err := Apply([]byte(sample), g)
		if err != nil {
			t.Fatalf("%s: %v", g, err)
		}
		if string(out) == sample {
			t.Fatalf("%s: mutation did not change the source — this is the failure mode that reads as an uncaught mutant", g)
		}
		if _, err := parser.ParseFile(token.NewFileSet(), "p.go", out, 0); err != nil {
			t.Errorf("%s produced unparseable source: %v\n%s", g, err, out)
		}
		// The original condition survives verbatim, so nothing it referenced
		// becomes unused.
		if !strings.Contains(string(out), g.Cond) {
			t.Errorf("%s dropped the original condition %q", g, g.Cond)
		}
	}
}

func TestApplyProducesTheIntendedTruthValue(t *testing.T) {
	guards, _ := Find("p.go", []byte(sample), Options{SkipErrNil: true})
	var sawNever, sawAlways bool
	for _, g := range guards {
		out, err := Apply([]byte(sample), g)
		if err != nil {
			t.Fatal(err)
		}
		switch g.Kind {
		case Never:
			sawNever = true
			if !strings.Contains(string(out), "("+g.Cond+") && false") {
				t.Errorf("Never mutant not shaped as expected:\n%s", out)
			}
		case Always:
			sawAlways = true
			if !strings.Contains(string(out), "("+g.Cond+") || true") {
				t.Errorf("Always mutant not shaped as expected:\n%s", out)
			}
		}
	}
	if !sawNever || !sawAlways {
		t.Error("did not exercise both directions")
	}
}

// Parenthesising matters: `a || b && false` is not `(a || b) && false`.
func TestPrecedenceIsPreserved(t *testing.T) {
	src := "package p\n\nfunc f(a, b bool) int {\n\tif a || b {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
	guards, err := Find("p.go", []byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range guards {
		out, _ := Apply([]byte(src), g)
		if !strings.Contains(string(out), "(a || b)") {
			t.Errorf("%s did not parenthesise a disjunction — the mutation would not be total:\n%s", g, out)
		}
	}
}

// The guards below were found by running this tool on itself — every one was a
// branch no test distinguished. Keeping them honest is the point: a coverage
// tool whose own guards are unexercised has no standing.

// isErrNil must reject the shapes that merely resemble `err != nil`, or
// SkipErrNil would silently drop real guards along with error plumbing.
func TestIsErrNilRejectsLookalikes(t *testing.T) {
	keep := []string{
		"if a != nil { return 1 }",            // not named err
		"if err != other { return 1 }",        // not compared to nil
		"if err() != nil { return 1 }",        // a call, not the identifier
		"if a > 0 { return 1 }",               // not a nil comparison at all
		"if !ok { return 1 }",                 // unary, not binary
		"if err != nil && a > 0 { return 1 }", // compound: the guard does more
	}
	for _, body := range keep {
		src := "package p\n\nfunc f(a int, err, other error, ok bool) int {\n\t" + body + "\n\treturn 0\n}\n"
		got, err := Find("p.go", []byte(src), Options{SkipErrNil: true})
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if len(got) == 0 {
			t.Errorf("SkipErrNil dropped a guard that is not error plumbing: %s", body)
		}
	}

	// And both directions of the real shape are dropped.
	for _, body := range []string{"if err != nil { return 1 }", "if err == nil { return 1 }"} {
		src := "package p\n\nfunc f(err error) int {\n\t" + body + "\n\treturn 0\n}\n"
		got, _ := Find("p.go", []byte(src), Options{SkipErrNil: true})
		if len(got) != 0 {
			t.Errorf("SkipErrNil kept error plumbing: %s → %v", body, got)
		}
	}
}

// Guards are reported in source order so a run reads top-to-bottom against the
// file, and so two guards on one line stay in a stable order.
func TestFindSortsByLineThenKind(t *testing.T) {
	src := "package p\n\nfunc f(a, b bool) int {\n\tif b {\n\t\treturn 2\n\t}\n\tif a {\n\t\treturn 1\n\t}\n\treturn 0\n}\n"
	got, err := Find("p.go", []byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Line < got[i-1].Line {
			t.Fatalf("guards out of source order: %v", got)
		}
		if got[i].Line == got[i-1].Line && got[i].Kind < got[i-1].Kind {
			t.Fatalf("guards on one line are not in a stable kind order: %v", got)
		}
	}
	if got[0].Cond != "b" {
		t.Errorf("first guard = %q, want the one that appears first in the file", got[0].Cond)
	}
}

// Apply must refuse a Guard whose offsets do not fit the source it is handed —
// otherwise a stale Guard silently corrupts a file it was not measured against.
func TestApplyRefusesOffsetsOutsideTheSource(t *testing.T) {
	guards, err := Find("p.go", []byte(sample), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply([]byte("package p\n"), guards[len(guards)-1]); err == nil {
		t.Error("Apply accepted a guard measured against different source — it would corrupt the file")
	}
	if _, err := Apply([]byte(sample), Guard{File: "p.go", Kind: Never, start: 5, end: 5}); err == nil {
		t.Error("Apply accepted an empty range")
	}
	if _, err := Apply([]byte(sample), Guard{File: "p.go", Kind: "sideways", start: 0, end: 4}); err == nil {
		t.Error("Apply accepted an unknown mutation kind")
	}
}

// A file whose conditions sit at the very edges must still be handled: the
// bounds check exists to reject impossible offsets, not valid ones.
func TestFindAcceptsAConditionAtTheFileEdge(t *testing.T) {
	src := "package p\n\nfunc f(a bool) { if a {\n} }\n"
	got, err := Find("p.go", []byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("found %d guards, want 2: %v", len(got), got)
	}
	for _, g := range got {
		if _, err := Apply([]byte(src), g); err != nil {
			t.Errorf("%s: %v", g, err)
		}
	}
}
