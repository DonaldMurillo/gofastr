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
  const r = (j) => { const s = document.createElement('script'); s.textContent = j; }; // 9 script text
  const u = (j) => { const s = document.createElement('script'); s.append(j); };       // ok
  const v = (el, a, b) => { el.innerHTML = _html(a) + b; };          // 10 call plus more
  const w = (el, a) => { el.innerHTML = _html(a); el.focus(); };     // ok
  const x = (u) => { const s = document.createElement('script'); s.setAttribute('src', u); }; // 11 attribute
  const z1 = (el, a) => { el.innerHTML = _html(a) ?? a ? a : ''; }; // 12 conditional after the fallback
  const z2 = (el, a) => { el.innerHTML = NS._html?.(a.b[0]) ?? a.b[0]; }; // ok
  const z4 = (el, a) => { el.innerHTML = _html(a) ?? a instanceof Object ? a : ''; }; // 14 word operator after the fallback
  const z5 = (el, a) => { el.innerHTML = _html(a) instanceof Object ? a : ''; }; // 15 word operator after the call
  const z6 = (el, a) => { el.innerHTML ||= a; };                    // 16 logical assignment
  const z7 = (el, a) => { el.innerHTML ??= _html(a); };             // ok
  const z3 = (u) => { const s = document.createElement('script'); s.setAttribute('src', u,); }; // 13 trailing comma
  const y = (u) => { const s = document.createElement('script'); s.setAttribute('src', _scriptURL(u)); s.setAttribute('type', 'module'); }; // ok
  const q = "el.innerHTML = x";                                     // ok: a string
  // el.innerHTML = raw in a comment is ok
})();
`

func TestLintTrustedTypesSinks(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.js"), []byte(trustedTypesFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	lintExpectFindings(t, LintTrustedTypesSinks, dir, 16, "a, d, e, g, i, k, n, o, r, v, x, z1, z3, z4, z5, z6", "[trusted-types-sink]")
}

func TestLintTrustedTypesSinks_RepoIsClean(t *testing.T) {
	lintRuntimeRepo(t, "shipped JavaScript hands the DOM a raw HTML string or script URL", LintTrustedTypesSinks)
}
