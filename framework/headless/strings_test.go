package headless

// The words gates: that every field is defaulted, that every word is
// reachable by some rendering, that the package says nothing in
// English outside Strings, and a second golden at the probe strings
// that proves every string on the page came through it.
//
// The mechanism for the corpus gates is wordsProbe (see W): the
// harness sets it and every component's defaults resolve to their
// probe tokens, so the whole fixture corpus renders through Strings
// without editing every Case. Strings set explicitly on a props still
// win, which is why WithSeams is rendered with its own probe too.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// TestEveryWordHasADefault: an empty field is a word nobody translated
// and a render that says nothing where it said something yesterday.
// Format fields must keep their placeholders, in order, in the probe,
// so a probe render still applies its arguments and the verb check
// below is the same comparison a translation will face.
func TestEveryWordHasADefault(t *testing.T) {
	w := DefaultStrings()
	val := reflect.ValueOf(*w)
	typ := val.Type()
	probe := reflect.ValueOf(*ProbeStrings())
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		def := val.Field(i).String()
		if def == "" {
			t.Errorf("%s has no default: an empty field is a word that renders as nothing", name)
			continue
		}
		want := placeholdersIn(def)
		got := placeholdersIn(probe.Field(i).String())
		if strings.Join(want, " ") != strings.Join(got, " ") {
			t.Errorf("%s: the probe keeps %q but the default carries %q — a render against the probe would drop an argument",
				name, strings.Join(got, " "), strings.Join(want, " "))
		}
	}
}

// probeCorpus renders every case of every spec, and every WithSeams
// fixture, with the probe words active, and returns the concatenated
// markup.
func probeCorpus(t *testing.T) string {
	t.Helper()
	prev := stringsProbe
	stringsProbe = ProbeStrings()
	t.Cleanup(func() { stringsProbe = prev })

	var b strings.Builder
	for _, sp := range Specs() {
		for _, c := range sp.Cases(Kit{}) {
			b.WriteString(string(c.HTML))
			b.WriteString("\n")
		}
		if sp.WithParts != nil {
			b.WriteString(string(sp.WithParts(nil, Parts{})))
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TestEveryWordIsSaidBySomeCase: a word no fixture says is a word
// nobody can see translated — the gate exists to make that visible
// rather than to be satisfied by deleting the assertion.
func TestEveryWordIsSaidBySomeCase(t *testing.T) {
	corpus := probeCorpus(t)

	val := reflect.ValueOf(*ProbeStrings())
	typ := val.Type()
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		// A plain field renders as its whole token; a format field
		// renders with its arguments after the name, so its token is
		// the name followed by a space. The angle brackets arrive
		// HTML-escaped in the markup. Matching on the name alone would
		// let Close stand in for CloseMenu.
		token := "&lt;" + name + "&gt;"
		if len(placeholdersIn(val.Field(i).String())) > 0 {
			token = "&lt;" + name + " "
		}
		if !strings.Contains(corpus, token) {
			t.Errorf("%s is said by no case: a word no fixture renders is a word nobody can see translated\n"+
				"add a case that says it, or remove the field", name)
		}
	}
}

// TestSpecGoldenStrings is the second golden: the same fixtures, the
// same order, every case rendered with the probe words. A real
// English word in it is a word that bypassed Strings — fixture
// labels (the English a Case chose on purpose) excepted, and those
// are the caller's strings, not the component's. Regenerate with
// GOFASTR_UPDATE_GOLDEN=1 after an intended change and read it once,
// end to end.
func TestSpecGoldenStrings(t *testing.T) {
	prev := stringsProbe
	stringsProbe = ProbeStrings()
	t.Cleanup(func() { stringsProbe = prev })

	var b strings.Builder
	for _, sp := range Specs() {
		for _, c := range sp.Cases(Kit{}) {
			b.WriteString("=== " + sp.Name + " / " + c.Name + " ===\n")
			b.WriteString(c.Why + "\n")
			b.WriteString(string(c.HTML))
			b.WriteString("\n\n")
		}
	}
	got := b.String()

	const path = "testdata/spec_golden_strings.txt"
	if os.Getenv("GOFASTR_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("writing words golden: %v", err)
		}
		return
	}
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading words golden: %v (run with GOFASTR_UPDATE_GOLDEN=1 to create)", err)
	}
	if string(golden) != got {
		t.Fatalf("the words of one or more components changed.\n"+
			"Every difference below is a string that stopped (or started) coming\n"+
			"through Strings. If it is intended, regenerate with\n"+
			"GOFASTR_UPDATE_GOLDEN=1 and read the diff.\n\n%s",
			firstDifference(string(golden), got))
	}
}

// allowedEnglish is what the walk below may still say in English
// outside Strings, each with its reason. Every entry must appear in
// the package's source or the test fails, so the list cannot outlive
// its reasons.
var allowedEnglish = map[string]string{}

// TestHeadlessSaysNothingInEnglishOutsideStrings: the go/ast walk from
// the inventory, now a gate. Outside init() (the fixtures, whose
// English is chosen on purpose) and outside words.go (the defaults
// themselves), no string literal that contains a space and a letter
// may be passed to render.Text, orDefault, an aria-label, title,
// placeholder or alt value, or fmt.Sprintf. A word that slips back in
// is a word a French page will say in English with no test the wiser.
func TestHeadlessSaysNothingInEnglishOutsideStrings(t *testing.T) {
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") ||
			name == "words.go" || strings.HasPrefix(name, ".") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name.Name == "init" {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := exprNameOf(call.Fun)
				var lits []string
				offence := ""
				switch callee {
				case "orDefault":
					if len(call.Args) == 2 {
						lits, offence = literalOperands(call.Args[1]), "an orDefault default"
					}
				case "render.Text":
					if len(call.Args) >= 1 {
						lits, offence = literalOperands(call.Args[0]), "render.Text"
					}
				case "render.HTML":
					if len(call.Args) >= 1 {
						lits, offence = literalOperands(call.Args[0]), "a render.HTML literal"
					}
				case "fmt.Sprintf", "Sprintf":
					if len(call.Args) >= 1 {
						lits, offence = literalOperands(call.Args[0]), "a fmt.Sprintf format"
					}
				}
				for _, l := range lits {
					if isEnglishPhrase(l) {
						report(t, fset, call.Pos(), l, offence, seen)
					}
				}
				// Attribute values the reader can be told.
				for _, a := range call.Args {
					checkAttrs(t, fset, a, seen)
				}
				return true
			})
			ast.Inspect(decl, func(n ast.Node) bool {
				if cl, ok := n.(*ast.CompositeLit); ok {
					checkAttrs(t, fset, cl, seen)
				}
				return true
			})
		}
	}
	for lit := range allowedEnglish {
		if !seen[lit] {
			t.Errorf("allowlisted %q no longer appears in the source — remove it or its reason is stale", lit)
		}
	}
}

func checkAttrs(t *testing.T, fset *token.FileSet, n ast.Node, seen map[string]bool) {
	cl, ok := n.(*ast.CompositeLit)
	if !ok {
		return
	}
	for _, el := range cl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, ok := kv.Key.(*ast.BasicLit)
		if !ok {
			continue
		}
		key := strings.ToLower(unquoteGo(k.Value))
		switch key {
		case "aria-label", "title", "placeholder", "alt":
		default:
			continue
		}
		if v, ok := kv.Value.(*ast.BasicLit); ok {
			lit := unquoteGo(v.Value)
			if isEnglishPhrase(lit) {
				report(t, fset, kv.Pos(), lit, "a "+key+" value", seen)
			}
		}
	}
}

func report(t *testing.T, fset *token.FileSet, pos token.Pos, lit, offence string, seen map[string]bool) {
	seen[lit] = true
	if reason, ok := allowedEnglish[lit]; ok {
		_ = reason
		return
	}
	t.Errorf("%s: %q is English said outside Strings (%s) — give it a Strings field",
		fset.Position(pos), lit, offence)
}

// literalOperands returns the string literals of e: the literal
// itself, or each literal side of a concatenation.
func literalOperands(e ast.Expr) []string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			return []string{unquoteGo(v.Value)}
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return append(literalOperands(v.X), literalOperands(v.Y)...)
		}
	}
	return nil
}

func exprNameOf(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprNameOf(v.X) + "." + v.Sel.Name
	}
	return ""
}

// isEnglishPhrase reports whether s looks like words rather than a
// machine value: a space and a letter. Single tokens ("polite",
// "text", ids, class names) cannot be told apart mechanically and are
// the inventory's job, not this gate's.
func isEnglishPhrase(s string) bool {
	space, letter := false, false
	for _, r := range s {
		if r == ' ' {
			space = true
		}
		if isLetter(r) {
			letter = true
		}
	}
	return space && letter
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 0x7f
}

func unquoteGo(s string) string {
	if out, err := strconv.Unquote(s); err == nil {
		return out
	}
	return s
}
