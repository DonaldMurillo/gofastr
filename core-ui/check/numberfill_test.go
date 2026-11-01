package check

import (
	"path/filepath"
	"strings"
	"testing"
)

// numberFillFixture is reduced from the C15/C18 sites (combobox,
// behavior, bell, sitesearch-page, localform) plus the fix spellings.
const numberFillFixture = `(() => {
  function bad(fmt, n, files, bell, total, q, s) {
    say(lb, fmt.replaceAll('{{n}}', () => String(n)));
    fmt.replaceAll('{{n}}', () => String(files.length));
    bell.setAttribute('aria-label', fmt.split('{{n}}').join(n));
    f.replace(/\{\{([nq])\}\}/g, function (_, k) { return k === 'n' ? String(total) : q; });
    return s.split('{{n}}').join(n === undefined ? '' : String(n));
  }
  function good(fmt, n, total, q, s) {
    say(lb, fmt.replaceAll('{{n}}', () => NS.formatNumber(n)));
    fmt.split('{{n}}').join(NS.formatNumber(n));
    fmt.replaceAll('{{name}}', () => String(n));
    fmt.replace('{{n}}', '0');
    f.replace(/\{\{([nq])\}\}/g, function (_, k) { return k === 'n' ? NS.formatNumber(total) : q; });
    f.replace(/\{\{q\}\}/g, () => q);
    f.replace(/\{\{([nq])\}\}/g, (_, k) => lookup[k]);
    s.split('{{q}}').join(q);
    s.split('{{n}}');
    s.split('{{n}}').map(String);
    s.split('{{n}}').filter(Boolean).join(sep);
  }
})();
`

func TestLintNumberFill_FiresOnASCIIFill(t *testing.T) {
	dir := writeRuntimeFixture(t, "fill.js", numberFillFixture)
	res, err := LintNumberFill(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 5 {
		t.Fatalf("expected 5 findings, got %d:\n%s", len(res.Violations), res.Error())
	}
	for _, w := range []string{"() => String(n)", "() => String(files.length)", "with n writes", "return k === 'n' ? String(total)", "n === undefined"} {
		if !strings.Contains(res.Error(), w) {
			t.Errorf("expected a finding naming %s (full result:\n%s)", w, res.Error())
		}
	}
}

// TestLintNumberFill_Repo holds the shipped JavaScript to the rule:
// every {{n}} fill goes through NS.formatNumber.
//
//gofastr:test-reads core-ui/runtime/
//gofastr:test-reads *.js
func TestLintNumberFill_Repo(t *testing.T) {
	if testing.Short() {
		t.Skip("walks the repo")
	}
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	res, err := LintNumberFill(runtimeLintRoots(t, filepath.Join(repoRoot, "core-ui", "runtime"))...)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("a {{n}} fill skips NS.formatNumber:\n%s", res.Error())
	}
}

// say() fills {{n}} from its params object: intercept.js's panel-limit
// toast passed { n: MAX_LAYERS } and wrote ASCII digits on every locale.
const sayFillFixture = `(() => {
  NS.say('runtime.panel_limit', '{{n}} panels is the limit', { n: MAX_LAYERS });
  say('k.left', '{{n}} left', { n });
  G.say('k.of', '{{n}} of {{t}}', { t: G.formatNumber(total), 'n': String(count) });
  say('k.ok', '{{n}} saved', { n: NS.formatNumber(x) });
  say('k.name', 'Hi {{name}}', { name: user });
  say('k.lit', '{{n}} max', { n: 3 });
  say('k.var', '{{n}} max', opts);
  say('k.q', 'for {{q}}', { q: n });
  say('k.none', 'Plain words');
})();
`

func TestNumberFillReadsSayParams(t *testing.T) {
	dir := writeRuntimeFixture(t, "say.js", sayFillFixture)
	res, err := LintNumberFill(dir)
	if err != nil {
		t.Fatal(err)
	}
	assertLines(t, res, []int{2, 3, 4})
}
