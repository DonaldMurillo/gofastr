package primitives

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
twins:
  - job: Sorting rows
    why: Export sorts a copy it may not keep.
    sites:
      - path: pkg/store/store.go
        note: sorts on write
      - path: pkg/heavy/heavy.go
        note: sorts on export
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
		"primitives/store/primitive.yml":        primitive,
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
	t.Cleanup(c.Close)
	findings = append(findings, c.Lint()...)
	var out []string
	for _, f := range findings {
		out = append(out, f.String())
	}
	return out
}

func TestValidRegistryIsClean(t *testing.T) {
	if got := lint(t, fixture(t, validPrimitive)); len(got) != 0 {
		t.Fatalf("valid registry has findings:\n%s", strings.Join(got, "\n"))
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
		{"twin without a reason", "    why: Export sorts a copy it may not keep.\n", "", "missing why"},
		{"twin with one site", "      - path: pkg/heavy/heavy.go\n        note: sorts on export\n", "", "names fewer than two sites"},
		{"twin site missing", "path: pkg/heavy/heavy.go", "path: pkg/heavy/gone.go", `site "pkg/heavy/gone.go" is not a file`},
		{"twin site without note", "        note: sorts on export\n", "", "missing note"},
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

func TestLoaderRefusals(t *testing.T) {
	for _, tc := range []struct{ name, file, body, want string }{
		{"a dot in a directory name", "primitives/shop.till/primitive.yml", "primitive: shop.till\nsummary: Takes money.\ncode:\n  - docs/store.md\n", `directory "shop.till" is not a kebab-case primitive name`},
		{"a blank list entry", "primitives/blank/primitive.yml", "primitive: blank\nsummary: Is blank.\ncode:\n  - docs/store.md\ndocs:\n  - \"\"\n", "docs entry is empty"},
		{"a list where a string belongs", "primitives/listy/primitive.yml", "primitive: listy\nsummary: [a, b]\ncode:\n  - docs/store.md\n", "summary must be a string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t, validPrimitive)
			write(t, root, tc.file, tc.body)
			got := strings.Join(lint(t, root), "\n")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("want a finding containing %q, got:\n%s", tc.want, got)
			}
			if tc.name == "a list where a string belongs" && strings.Contains(got, "summary is empty") {
				t.Errorf("a non-scalar value drew a second, wrong finding:\n%s", got)
			}
		})
	}
}

func TestUnparsedParentIsNotCalledMissing(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "primitives/shop/primitive.yml", "primitive: shop\nsummary: Sells.\ncode:\n  - docs/store.md\n   bad: indent\n")
	write(t, root, "primitives/shop/till/primitive.yml", "primitive: shop.till\nsummary: Takes money.\ncode:\n  - docs/store.md\n")
	got := strings.Join(lint(t, root), "\n")
	if strings.Contains(got, "has no primitive.yml") {
		t.Errorf("a parent that failed to parse was reported as missing:\n%s", got)
	}
	if !strings.Contains(got, "primitives/shop/primitive.yml:5:") {
		t.Errorf("the parse error should carry its own line, got:\n%s", got)
	}
}

func TestChildNeedsParent(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "primitives/shop/till/primitive.yml", "primitive: shop.till\nsummary: Takes money.\ncode:\n  - docs/store.md\n")
	if got := strings.Join(lint(t, root), "\n"); !strings.Contains(got, `parent primitive "shop" has no primitive.yml`) {
		t.Fatalf("got:\n%s", got)
	}
}

func TestFileHasOneOwnerChain(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "primitives/rival/primitive.yml", "primitive: rival\nsummary: Also keeps rows.\ncode:\n  - pkg/store/store.go\n")
	if got := strings.Join(lint(t, root), "\n"); !strings.Contains(got, "pkg/store/store.go is claimed by both") {
		t.Fatalf("got:\n%s", got)
	}
	root = fixture(t, validPrimitive)
	write(t, root, "primitives/store/rows/primitive.yml", "primitive: store.rows\nsummary: One row.\ncode:\n  - pkg/store/store.go\n")
	if got := lint(t, root); len(got) != 0 {
		t.Fatalf("a child may narrow its parent's files, got:\n%s", strings.Join(got, "\n"))
	}
}

func TestChainPicksTheDeepestOwner(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "primitives/store/rows/primitive.yml", "primitive: store.rows\nsummary: One row.\ncode:\n  - pkg/store/store.go\ninvariants:\n  - id: row-is-whole\n    rule: A row is whole.\n    judge: Is a row partial?\n")
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
	if got := c.TwinsOf("pkg/heavy/heavy.go"); len(got) != 1 || got[0].Job != "Sorting rows" {
		t.Errorf("twins of an unowned site = %+v", got)
	}
	if got := c.TwinsOf("docs/store.md"); len(got) != 0 {
		t.Errorf("twins of a file in no entry = %+v", got)
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

func TestACommentedOutTestEnforcesNothing(t *testing.T) {
	root := fixture(t, validPrimitive)
	write(t, root, "pkg/store/store_test.go", "package store\n\n/*\nfunc TestOrder(t *testing.T) {}\n*/\n")
	if got := strings.Join(lint(t, root), "\n"); !strings.Contains(got, "test TestOrder does not exist in pkg/store") {
		t.Fatalf("a test inside a block comment was accepted as enforcing:\n%s", got)
	}
}

func TestModulePathIgnoresATrailingComment(t *testing.T) {
	for name, gomod := range map[string]string{
		"a tab and a trailing comment": "module\texample.test/m // the shop\n",
		"a quoted path":                "module \"example.test/m\"\n",
		"a quoted path and a comment":  "module \"example.test/m\" // the shop\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := fixture(t, validPrimitive)
			write(t, root, "go.mod", gomod)
			c, _, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(c.Close)
			files, err := c.Generate()
			if err != nil {
				t.Fatal(err)
			}
			if src := string(files["pkg/store/"+ImportsTestFile]); !strings.Contains(src, `"example.test/m/pkg/heavy"`) {
				t.Errorf("the module path was not read cleanly:\n%s", src)
			}
		})
	}
}

func TestAHelperNamedLikeATestEnforcesNothing(t *testing.T) {
	root := fixture(t, strings.Replace(validPrimitive, "pkg/store.TestOrder", "pkg/store.Testorder", 1))
	write(t, root, "pkg/store/store_test.go", "package store\n\nfunc Testorder(t *testing.T) {}\n")
	if got := strings.Join(lint(t, root), "\n"); !strings.Contains(got, "test Testorder does not exist in pkg/store") {
		t.Fatalf("a function go test does not run was accepted as enforcing:\n%s", got)
	}
}
