package ownstyle

import (
	"os"
	"slices"
	"testing"
)

func modelFile(t *testing.T, file string) *SheetModel {
	t.Helper()
	src, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	sheet, diags := Parse(string(src))
	if len(diags) != 0 {
		t.Fatalf("parse diags: %v", diags)
	}
	m, errs := Model(sheet)
	if len(errs) != 0 {
		t.Fatalf("model errors: %v", errs)
	}
	return m
}

func TestModelIssuecard(t *testing.T) {
	m := modelFile(t, "issuecard.style.css")
	if m.Root == nil {
		t.Fatal("no root model")
	}
	if m.Root.Doc != "One issue: key, title, meta." {
		t.Errorf("root doc: %q", m.Root.Doc)
	}
	if len(m.Root.Flags) != 1 || m.Root.Flags[0].Name != "fresh" {
		t.Errorf("root flags: %+v", m.Root.Flags)
	}
	if m.Root.Flags[0].Doc != "A flag: changed since the viewer last looked." {
		t.Errorf("fresh doc: %q", m.Root.Flags[0].Doc)
	}
	if len(m.Root.Groups) != 1 || m.Root.Groups[0] != "priority" {
		t.Errorf("root groups: %+v", m.Root.Groups)
	}
	if len(m.Groups) != 1 || m.Groups[0].Name != "priority" {
		t.Fatalf("groups: %+v", m.Groups)
	}
	g := m.Groups[0]
	if g.Doc != "A group: one priority at a time." {
		t.Errorf("group doc: %q", g.Doc)
	}
	if len(g.Values) != 3 || g.Values[0].Value != "urgent" || g.Values[1].Value != "high" || g.Values[2].Value != "low" {
		t.Errorf("group values: %+v", g.Values)
	}
	// Key/Title/Meta are plain classes, in first-appearance order after
	// the :scope-family rules they follow in the source.
	var names []string
	for _, c := range m.Classes {
		names = append(names, c.Name)
	}
	if len(m.Classes) != 3 {
		t.Fatalf("classes: %v (want key,title,meta)", names)
	}
	for _, want := range []string{"key", "title", "meta"} {
		found := false
		for _, c := range m.Classes {
			found = found || c.Name == want
		}
		if !found {
			t.Errorf("class %s missing from %v", want, names)
		}
	}
	for _, c := range m.Classes {
		if len(c.Flags) != 0 || len(c.Groups) != 0 {
			t.Errorf("class %s carries variants: %+v", c.Name, c)
		}
	}
}

func TestModelBoard(t *testing.T) {
	m := modelFile(t, "board.style.css")
	if m.Root != nil {
		t.Errorf("board :scope carries no variants, got root %+v", m.Root)
	}
	var column *ClassModel
	for _, c := range m.Classes {
		if c.Name == "column" {
			column = c
		}
	}
	if column == nil {
		t.Fatalf("no column base in %v", classNames(m))
	}
	if len(column.Flags) != 1 || column.Flags[0].Name != "over-limit" {
		t.Fatalf("column flags: %+v", column.Flags)
	}
	if column.Flags[0].Doc != "A flag: more cards than the column's WIP limit." {
		t.Errorf("over-limit doc: %q", column.Flags[0].Doc)
	}
	// The descendant compound ".column.over-limit .count" contributes
	// the .count base too.
	if m.class("count") == nil {
		t.Errorf("count base missing from %v", classNames(m))
	}
}

func TestModelApp(t *testing.T) {
	m := modelFile(t, "app.style.css")
	if m.Root != nil {
		t.Errorf("app sheet has no :scope variants, got %+v", m.Root)
	}
	var priority *ClassModel
	for _, c := range m.Classes {
		if c.Name == "priority" {
			priority = c
		}
	}
	if priority == nil {
		t.Fatalf("no priority base in %v", classNames(m))
	}
	if len(priority.Groups) != 1 || priority.Groups[0] != "level" {
		t.Fatalf("priority groups: %+v", priority.Groups)
	}
	if len(m.Groups) != 1 || m.Groups[0].Name != "level" {
		t.Fatalf("groups: %+v", m.Groups)
	}
	if len(m.Groups[0].Values) != 2 ||
		m.Groups[0].Values[0].Value != "urgent" || m.Groups[0].Values[1].Value != "high" {
		t.Fatalf("level values: %+v", m.Groups[0].Values)
	}
}

func classNames(m *SheetModel) []string {
	out := make([]string, 0, len(m.Classes))
	for _, c := range m.Classes {
		out = append(out, c.Name)
	}
	return out
}

func TestModelClassBothBaseAndVariantIsError(t *testing.T) {
	src := ".a.b { color: red; }\n.b.c { color: red; }\n"
	sheet, _ := Parse(src)
	_, diags := Model(sheet)
	if len(diags) != 1 || diags[0].Rule != RuleClassTwice {
		t.Fatalf("want one class-base-and-variant, got %v", diags)
	}
	if diags[0].Line != 2 || diags[0].Col != 1 {
		t.Fatalf("position: %d:%d", diags[0].Line, diags[0].Col)
	}
	if diags[0].Message != ".b is used as both a base and a variant in one file; a class is one or the other" {
		t.Fatalf("message: %s", diags[0].Message)
	}
}

// A class inside :has() names another element, so it is a base of its
// own, never a variant of the compound around it. Inside :not() the
// class still describes the same element: a variant.
func TestModelHasArgumentIsItsOwnBase(t *testing.T) {
	src := ":scope:has(.toc > :empty) { color: red; }\n" +
		".toc:has(> :empty) { color: red; }\n" +
		".grid:has(.cell.wide) { color: red; }\n" +
		".page:not(.next) { color: red; }\n.page.next { color: red; }\n"
	sheet, _ := Parse(src)
	m, diags := Model(sheet)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if got := classNames(m); !slices.Equal(got, []string{"toc", "grid", "cell", "page"}) {
		t.Fatalf("classes = %v", got)
	}
	if m.Root != nil && len(m.Root.Flags) != 0 {
		t.Errorf(":scope picked up flags from inside :has(): %+v", m.Root.Flags)
	}
	for _, c := range m.Classes {
		var flags []string
		for _, f := range c.Flags {
			flags = append(flags, f.Name)
		}
		want := map[string][]string{"cell": {"wide"}, "page": {"next"}}[c.Name]
		if !slices.Equal(flags, want) {
			t.Errorf("%s flags = %v, want %v", c.Name, flags, want)
		}
	}
}

func TestModelGroupValueMismatchIsError(t *testing.T) {
	src := ".a.level--urgent { color: red; }\n.a.level--high { color: red; }\n" +
		".b.level--urgent { color: red; }\n.b.level--low { color: red; }\n"
	sheet, _ := Parse(src)
	m, diags := Model(sheet)
	if len(diags) != 1 || diags[0].Rule != RuleGroupMismatch {
		t.Fatalf("want one group-value-mismatch, got %v", diags)
	}
	// Same group under one base only: fine.
	if got := len(m.Groups[0].Values); got != 3 {
		t.Errorf("values folded across bases: %d", got)
	}
	// The same set under two bases is fine.
	okSrc := ".a.g--x {}\n.a.g--y {}\n.b.g--x {}\n.b.g--y {}\n"
	sheet2, _ := Parse(okSrc)
	if _, diags2 := Model(sheet2); len(diags2) != 0 {
		t.Errorf("matching sets reported: %v", diags2)
	}
}

func TestModelIgnoresKitAndElementParts(t *testing.T) {
	src := ".card .fui-card__body { gap: 0; }\nh2.title { color: red; }\n"
	sheet, _ := Parse(src)
	m, diags := Model(sheet)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	for _, c := range m.Classes {
		if c.Name == "fui-card__body" {
			t.Errorf("kit class entered the model")
		}
	}
	// .title IS a class of the compound (first class of h2.title).
	if m.class("title") == nil {
		t.Errorf("title missing from %v", classNames(m))
	}
}

// A rule's doc comment describes the compound it styles, not every
// class its selector mentions: ".column.over .count" documents the
// over flag, and .count takes its doc from its own rule.
func TestModelDocStaysOnFirstCompound(t *testing.T) {
	sheet, _ := Parse(`/* A flag: over the limit. */
.column.over .count { color: red; }
/* The card count. */
.count { font-size: 1px; }`)
	m, diags := Model(sheet)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if got := m.class("count").Doc; got != "The card count." {
		t.Errorf("count doc: %q", got)
	}
	if got := m.class("column").Flags[0].Doc; got != "A flag: over the limit." {
		t.Errorf("over doc: %q", got)
	}
}
