package docs

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The runtime attribute tables in core-ui/ARCHITECTURE.md (the repo's
// source of truth for the UI/runtime contract) and the embedded extract
// content/runtime-contract.md (what `gofastr docs` readers see) must
// document the same attributes, every family: data-cui-*, data-hui-*,
// data-fui-*, data-search-* and the rest. Until round 8 the gate read only
// data-cui-* rows and matched with strings.Contains, so data-cui-compute
// passed because data-cui-computed contains it, and the two tables had
// drifted apart by some sixty rows the gate never saw.

// attrToken is one attribute name in a table row's first cell. A trailing
// `*` or `-*` names a family (`data-hui-sortable-s-*`) and is part of the
// token, so a family row is matched as a family.
var attrToken = regexp.MustCompile(`data-[a-z0-9]+(?:-[a-z0-9]+)*(?:-?\*)?`)

// rowAttrs returns the attribute names leading each attribute-table row.
func rowAttrs(doc string) []string {
	var out []string
	for ln := range strings.SplitSeq(doc, "\n") {
		if !strings.HasPrefix(ln, "| `data-") {
			continue
		}
		cells := strings.SplitN(ln, "|", 3)
		if len(cells) < 3 {
			continue
		}
		out = append(out, attrToken.FindAllString(cells[1], -1)...)
	}
	return out
}

// mentions reports whether doc names attr as a whole token: not as the
// prefix of a longer attribute (data-cui-compute in data-cui-computed).
func mentions(doc, attr string) bool {
	re := regexp.MustCompile(regexp.QuoteMeta(attr) + `(?:[^a-z0-9*-]|$)`)
	return re.MatchString(doc)
}

func readArchitecture(t *testing.T) string {
	t.Helper()
	arch, err := os.ReadFile(filepath.Join("..", "..", "core-ui", "ARCHITECTURE.md"))
	if err != nil {
		// Outside the repo checkout (embedded-only consumers) there is
		// nothing to sync against.
		t.Skipf("core-ui/ARCHITECTURE.md not readable: %v", err)
	}
	return string(arch)
}

func readRuntimeContract(t *testing.T) string {
	t.Helper()
	embedded, err := Get("runtime-contract")
	if err != nil {
		t.Fatalf("embedded runtime-contract doc missing: %v", err)
	}
	return string(embedded)
}

// missingFrom lists the attributes leading a row of src that dst never
// names as a whole token.
func missingFrom(src, dst string) []string {
	var missing []string
	for _, attr := range rowAttrs(src) {
		if !mentions(dst, attr) {
			missing = append(missing, attr)
		}
	}
	slices.Sort(missing)
	return slices.Compact(missing)
}

// TestRuntimeContractCoversArchitectureAttrs fails when ARCHITECTURE.md
// documents an attribute the extract does not.
//
//gofastr:test-reads core-ui/ARCHITECTURE.md
func TestRuntimeContractCoversArchitectureAttrs(t *testing.T) {
	arch := readArchitecture(t)
	embedded := readRuntimeContract(t)
	if len(rowAttrs(arch)) == 0 {
		t.Fatal("no data-* table rows found in core-ui/ARCHITECTURE.md — did the attribute table move? Update this parser and content/runtime-contract.md together")
	}
	if missing := missingFrom(arch, embedded); len(missing) > 0 {
		t.Errorf("content/runtime-contract.md is out of sync with core-ui/ARCHITECTURE.md — missing attributes: %s\n(the extract must be updated in the same commit as the ARCHITECTURE table; see the SYNC NOTE in runtime-contract.md)", strings.Join(missing, ", "))
	}
}

// TestRuntimeContractAttrsExistUpstream is the inverse: every attribute
// leading a row of the extract must be documented in ARCHITECTURE.md too.
// The extract once kept a `data-cui-fill-hash` row after the fill hash was
// dropped (nothing emitted or read it; every kept-layer fill is
// re-applied), so a reader built a client around a skip the runtime never
// performs.
//
//gofastr:test-reads core-ui/ARCHITECTURE.md
func TestRuntimeContractAttrsExistUpstream(t *testing.T) {
	arch := readArchitecture(t)
	embedded := readRuntimeContract(t)
	if phantom := missingFrom(embedded, arch); len(phantom) > 0 {
		t.Errorf("content/runtime-contract.md documents attributes core-ui/ARCHITECTURE.md does not: %s", strings.Join(phantom, ", "))
	}
}

// singleBraceFill is a `{name}` token: the runtime modules fill `{{name}}`
// (i18n's placeholder spelling), so a row that writes single braces tells
// a host to hand-write an attribute whose placeholder is never replaced.
// A path parameter (`/orders/{id}`) is not a fill token.
var singleBraceFill = regexp.MustCompile(`(?:^|[^{/$])\{([a-z][A-Za-z0-9_]*)\}(?:[^}]|$)`)

// printfFill is a Go verb standing in for a fill token.
var printfFill = regexp.MustCompile(`%[sdvq]`)

// TestAttrRowsSpellDoubleBraceFills fails on an attribute-table row that
// spells a fill token `{x}` or `%s` where the module replaces `{{x}}`.
//
//gofastr:test-reads core-ui/ARCHITECTURE.md
func TestAttrRowsSpellDoubleBraceFills(t *testing.T) {
	docs := map[string]string{
		"core-ui/ARCHITECTURE.md":                    readArchitecture(t),
		"framework/docs/content/runtime-contract.md": readRuntimeContract(t),
	}
	for _, name := range slices.Sorted(maps.Keys(docs)) {
		for i, ln := range strings.Split(docs[name], "\n") {
			if !strings.HasPrefix(ln, "| `data-") {
				continue
			}
			if m := singleBraceFill.FindStringSubmatch(ln); m != nil {
				t.Errorf("%s:%d: fill token {%s} — the modules replace {{%s}}", name, i+1, m[1], m[1])
			}
			if m := printfFill.FindString(ln); m != "" {
				t.Errorf("%s:%d: fill token %s — the modules replace {{name}} tokens, not printf verbs", name, i+1, m)
			}
		}
	}
}
