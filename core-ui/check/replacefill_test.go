package check

import (
	"path/filepath"
	"strings"
	"testing"
)

// replaceFillFixtureRaw uses ~ for JS backticks; untailed below. The
// positives are reduced from the pre-fix framework/headless sources
// (collections.js makeChip/commit, feedback.js toast dismiss, the
// drop zone's names); the negatives are the fix spelling and the
// author-written patterns the rule must leave alone.
const replaceFillFixtureRaw = `(() => {
  function makeChip(root, v) {
    const labelFmt = root.getAttribute('data-hui-tag-input-remove-label') || '';
    li.setAttribute('aria-label', labelFmt.replace('%s', v));
    say(status, (status.getAttribute('data-hui-tag-input-added') || '').replace('{name}', val));
    status.textContent = fmt.replace('{names}', names.join(', '));
    meta.replace(/([?&]session=)[^&]*/, '$1' + sessionId);
    return tpl.replaceAll('{label}', label);
  }
  function fixed(root, v) {
    li.setAttribute('aria-label', labelFmt.replace('%s', () => v));
    tpl.replace(/\{(label|list)\}/g, function (_, k) { return k; });
    meta.replace(/([?&]session=)[^&]*/, (_, p) => p + sessionId);
    s.replace(/([!"#$%&'()*+,./:;<=>?@[\]^~{|}])/g, '\\$1');
    s.replace(/x/g, ~y~);
    s.replace(/x/g, 0);
    location.replace(path);
    el.classList.replace('a', b);
  }
})();
`

var replaceFillFixture = strings.ReplaceAll(replaceFillFixtureRaw, "~", "\x60")

func TestLintReplaceFill_FiresOnComputed(t *testing.T) {
	dir := writeRuntimeFixture(t, "fill.js", replaceFillFixture)
	res, err := LintReplaceFill(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 5 {
		t.Fatalf("expected 5 findings, got %d:\n%s", len(res.Violations), res.Error())
	}
	for _, w := range []string{"v", "val", "names.join(', ')", "'$1' + sessionId", "label"} {
		if !strings.Contains(res.Error(), ".replace(…, "+w+") inserts") {
			t.Errorf("expected a finding naming %s (full result:\n%s)", w, res.Error())
		}
	}
}

func TestLintReplaceFill_RepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	runtimeDir := filepath.Join(repoRoot, "core-ui", "runtime")
	res, err := LintReplaceFill(runtimeLintRoots(t, runtimeDir)...)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("runtime JS fills a template with a computed replacement string:\n%s", res.Error())
	}
}
