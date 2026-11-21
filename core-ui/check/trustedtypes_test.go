package check

import (
	"os"
	"path/filepath"
	"testing"
)

const trustedTypesFixture = `(() => {
  const NS = window.__gofastr;
  const a = (el, html) => { el.innerHTML = html; };                 // 1 raw
  const b = (el, html) => { el.innerHTML = _html(html); };          // ok
  const c = (el, html) => { el.innerHTML = NS._html?.(html) ?? html; }; // ok
  const d = (el, html) => { el.innerHTML += _html(html); };         // 2 append
  const e = (el, html) => { el.insertAdjacentHTML('beforeend', html); }; // 3 raw
  const f = (el, html) => { el.insertAdjacentHTML('beforeend', _html(html)); }; // ok
  const g = (s) => new DOMParser().parseFromString(s, 'text/html'); // 4 raw
  const h = (s) => new DOMParser().parseFromString(_html(s), 'text/html'); // ok
  const i = (u) => new Worker(u);                                   // 5 raw
  const j = (u) => new Worker(NS._scriptURL?.(u) ?? u);             // ok
  const k = (u) => { const s = document.createElement('script'); s.src = u; };          // 6 raw
  const l = (u) => { const s = document.createElement('script'); s.src = _scriptURL(u); }; // ok
  const m = (img, u) => { img.src = u; };                           // ok: not a script
  const n = () => document.write('<p>');                            // 7
  const o = (s) => eval(s);                                         // 8
  const p = (el) => el.innerHTML === '';                            // ok: a read
  const q = "el.innerHTML = x";                                     // ok: a string
  // el.innerHTML = raw in a comment is ok
})();
`

func TestLintTrustedTypesSinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.js"), []byte(trustedTypesFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	lintExpectFindings(t, LintTrustedTypesSinks, dir, 8, "a, d, e, g, i, k, n, o", "[trusted-types-sink]")
}

func TestLintTrustedTypesSinks_RepoIsClean(t *testing.T) {
	lintRuntimeRepo(t, "shipped JavaScript hands the DOM a raw HTML string or script URL", LintTrustedTypesSinks)
}
