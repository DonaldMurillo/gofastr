package canon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validPrimitive = `primitive: store
summary: Keeps rows.
code:
  - pkg/store/
docs:
  - docs/store.md
imports:
  - deny: pkg/heavy
    from: [pkg/store]
    why: It is heavy.
    instead: Use the interface.
invariants:
  - id: rows-keep-order
    rule: Rows keep their order.
    enforced_by:
      - test: pkg/store.TestOrder
      - analyzer: orderly
      - contract: RULE1
  - id: judged-only
    rule: Names are kind.
    judge: Is a name unkind?
decisions:
  - id: old-way
    status: superseded
    superseded_by: new-way
    decision: Sort on read.
  - id: new-way
    date: 2026-11-14
    status: accepted
    decision: Sort on write.
    rejected:
      - option: Sort on read.
        why: It was slow.
    revisit_if: Reads outnumber writes.
    upholds: [rows-keep-order]
`

func fixture(t *testing.T, primitive string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":                                "module example.test/m\n",
		"canon/store/primitive.yml":             primitive,
		"docs/store.md":                         "# store\n",
		"pkg/store/store.go":                    "package store\n",
		"pkg/store/store_test.go":               "package store\n\nfunc TestOrder(t *testing.T) {}\n",
		"pkg/heavy/heavy.go":                    "package heavy\n",
		"internal/analyzers/orderly/orderly.go": "package orderly\n",
		"internal/analyzers/suite/suite.go":     "package suite\n\nimport _ \"example.test/m/internal/analyzers/orderly\"\n",
		"framework/contracts/catalog.go":        "package contracts\n\nconst Rule = \"RULE1\"\n",
	} {
		write(t, root, name, body)
	}
	return root
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func lint(t *testing.T, root string) []string {
	t.Helper()
	c, findings, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	findings = append(findings, c.Lint()...)
	var out []string
	for _, f := range findings {
		out = append(out, f.String())
	}
	return out
}

func TestValidCanonIsClean(t *testing.T) {
	if got := lint(t, fixture(t, validPrimitive)); len(got) != 0 {
		t.Fatalf("valid canon has findings:\n%s", strings.Join(got, "\n"))
	}
}

func TestLintRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, old, new, want string
	}{
		{"unknown key", "summary: Keeps rows.", "summary: Keeps rows.\nowner: me", `unknown key "owner"`},
		{"missing summary", "summary: Keeps rows.\n", "", "missing summary"},
		{"id does not match directory", "primitive: store", "primitive: shop", `the directory makes it "store"`},
		{"code matches nothing", "pkg/store/\n", "pkg/gone/\n", `code "pkg/gone/" matches no file`},
		{"docs missing", "docs/store.md", "docs/gone.md", `docs "docs/gone.md" is not a file`},
		{"deny is no package", "deny: pkg/heavy", "deny: pkg/gone", `deny "pkg/gone" is not a Go package`},
		{"from is no package", "from: [pkg/store]", "from: [pkg/gone]", `from "pkg/gone" is not a Go package`},
		{"imports without instead", "    instead: Use the interface.\n", "", "missing instead"},
		{"invariant id not kebab", "id: rows-keep-order", "id: Rows_Keep", "must be kebab-case"},
		{"missing rule", "    rule: Rows keep their order.\n", "", "missing rule"},
		{"test does not exist", "TestOrder", "TestGone", "test TestGone does not exist in pkg/store"},
		{"test without package", "pkg/store.TestOrder", "TestOrder", "must be <package dir>.<TestName>"},
		{"analyzer has no package", "analyzer: orderly", "analyzer: gone", `analyzer "gone" has no package`},
		{"contract not in catalog", "contract: RULE1", "contract: RULE9", `contract rule "RULE9" is not in`},
		{"two enforcers in one entry", "      - analyzer: orderly\n", "        analyzer: orderly\n", "exactly one of"},
		{"nothing enforces", "    judge: Is a name unkind?\n", "", "no enforced_by and no judge"},
		{"judge is not a question", "Is a name unkind?", "Names must be kind.", "must be a yes/no question"},
		{"bad status", "status: accepted", "status: maybe", `status "maybe"`},
		{"superseded without successor", "    superseded_by: new-way\n", "", "superseded_by must name another decision"},
		{"accepted with successor", "status: accepted", "status: accepted\n    superseded_by: old-way", "is accepted but names superseded_by"},
		{"bad date", "2026-11-14", "14 Nov", "must be YYYY-MM-DD"},
		{"upholds nothing", "upholds: [rows-keep-order]", "upholds: [gone]", `upholds "gone" names no invariant`},
		{"duplicate invariant", "id: judged-only", "id: rows-keep-order", `invariant "rows-keep-order" is declared twice`},
		{"duplicate decision", "id: old-way", "id: new-way", `decision "new-way" is declared twice`},
		{"rejected without why", "        why: It was slow.\n", "", "missing why"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(validPrimitive, tc.old) {
				t.Fatalf("fixture no longer holds %q", tc.old)
			}
			got := strings.Join(lint(t, fixture(t, strings.Replace(validPrimitive, tc.old, tc.new, 1))), "\n")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want a finding containing %q, got:\n%s", tc.want, got)
			}
		})
	}
}

func TestUnregisteredAnalyzerIsRefused(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "internal/analyzers/suite/suite.go", "package suite\n")
	if got := strings.Join(lint(t, root), "\n"); !strings.Contains(got, `analyzer "orderly" is not registered`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestChildNeedsParent(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "canon/shop/till/primitive.yml", "primitive: shop.till\nsummary: Takes money.\ncode:\n  - docs/store.md\n")
	if got := strings.Join(lint(t, root), "\n"); !strings.Contains(got, `parent primitive "shop" has no primitive.yml`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestFileHasOneOwnerChain(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "canon/rival/primitive.yml", "primitive: rival\nsummary: Also keeps rows.\ncode:\n  - pkg/store/store.go\n")
	if got := strings.Join(lint(t, root), "\n"); !strings.Contains(got, "pkg/store/store.go is claimed by both") {
		t.Fatalf("got:\n%s", got)
	}
	root = fixture(t, validPrimitive)
	write(t, root, "canon/store/rows/primitive.yml", "primitive: store.rows\nsummary: One row.\ncode:\n  - pkg/store/store.go\n")
	if got := lint(t, root); len(got) != 0 {
		t.Fatalf("a child may narrow its parent's files, got:\n%s", strings.Join(got, "\n"))
	}
}

func TestChainPicksTheDeepestOwner(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "canon/store/rows/primitive.yml", "primitive: store.rows\nsummary: One row.\ncode:\n  - pkg/store/store.go\ninvariants:\n  - id: row-is-whole\n    rule: A row is whole.\n    judge: Is a row partial?\n")
	c, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(file string) string {
		var out []string
		for _, p := range c.Chain(file) {
			out = append(out, p.ID)
		}
		return strings.Join(out, " > ")
	}
	if got := ids("pkg/store/store.go"); got != "store > store.rows" {
		t.Errorf("store.go chain = %q", got)
	}
	if got := ids("pkg/store/store_test.go"); got != "store" {
		t.Errorf("store_test.go chain = %q", got)
	}
	if got := ids("pkg/heavy/heavy.go"); got != "" {
		t.Errorf("unowned file chain = %q", got)
	}
	checks := c.Checks([]string{"pkg/store/store.go", "pkg/store/store_test.go", "pkg/heavy/heavy.go"})
	if len(checks) != 2 {
		t.Fatalf("want 2 checks, got %+v", checks)
	}
	if checks[0].Invariant != "judged-only" || strings.Join(checks[0].Files, ",") != "pkg/store/store.go,pkg/store/store_test.go" {
		t.Errorf("parent check = %+v", checks[0])
	}
	if checks[1].Invariant != "row-is-whole" || strings.Join(checks[1].Files, ",") != "pkg/store/store.go" {
		t.Errorf("child check = %+v", checks[1])
	}
}

func TestGenerateAndStale(t *testing.T) {
	root := fixture(t, validPrimitive)
	c, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate()
	if err != nil {
		t.Fatal(err)
	}
	const rel = "pkg/store/" + ImportsTestFile
	src := string(files[rel])
	for _, want := range []string{"package store_test", `"example.test/m/pkg/heavy"`, `"It is heavy."`, "DO NOT EDIT"} {
		if !strings.Contains(src, want) {
			t.Errorf("generated test lacks %q:\n%s", want, src)
		}
	}
	state := func() string {
		stale, err := c.Stale()
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for k, v := range stale {
			out = append(out, k+"="+v)
		}
		return strings.Join(out, ",")
	}
	if got := state(); got != rel+"=missing" {
		t.Errorf("before writing: %q", got)
	}
	write(t, root, rel, src)
	if got := state(); got != "" {
		t.Errorf("after writing: %q", got)
	}
	write(t, root, rel, src+"// edited\n")
	if got := state(); got != rel+"=outdated" {
		t.Errorf("after editing: %q", got)
	}
	write(t, root, rel, src)
	write(t, root, "pkg/heavy/"+ImportsTestFile, src)
	if got := state(); got != "pkg/heavy/"+ImportsTestFile+"=orphaned" {
		t.Errorf("with a stray file: %q", got)
	}
}
