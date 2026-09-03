package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixtures for the four runtime-shape lints. Every positive case is
// reduced from the pre-fix sources at commit 7bd789e9 (the shapes the
// adversarial-probe audit found, real API names kept); every negative
// case is the fixed spelling from e936f791; each file also carries at
// least two synthetic positives — the same shape under names that never
// existed in this repo — so the rules are proven shape-wise, not
// site-wise. The whole-repo posture is pinned separately by the
// RepoIsClean tests at the bottom.

func writeRuntimeFixture(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// selectorFixture uses ~ for JS backticks (Go raw strings cannot carry
// them); untailed below.
const selectorFixtureRaw = `// Reduced from the pre-fix sources at 7bd789e9 (real API names):
// rangeslider.js, conditionalfield.js, multiselect.js, slider.js,
// carousel.js, widgets.js, frag/kernel.js, frag/boot.js.
(() => {
  'use strict';

  function pairFor(id) { // rangeslider.js, pre-fix: 2 lookups, 1 helper
    return {
      min: document.querySelector('input[data-fui-range-slider="' + id + '"].ui-range-slider__input--min'),
      out: document.querySelector('output[data-fui-range-slider-value="' + id + '"]'),
    };
  }

  function wireField(form, whenName) { // conditionalfield.js, pre-fix
    return form.querySelectorAll('[name="' + whenName + '"]');
  }

  function labelFor(root, cb) { // multiselect.js, pre-fix
    return root.querySelector('label[for="' + cb.id + '"] .ui-multiselect__row-label');
  }

  function mirrorFor(id) { // slider.js, pre-fix
    return document.querySelector('output[for="' + id + '"]');
  }

  function deferFor(carousel, id, k) { // carousel.js, pre-fix
    carousel.querySelector('script[type="application/json"][data-fui-carousel-deferred-for="' + id + '"]');
    return carousel.querySelector('[data-fui-carousel-defer="' + k + '"]');
  }

  function styleLoaded(name) { // frag/kernel.js, pre-fix
    return !!document.querySelector('link[data-fui-style="' + name + '"]');
  }

  const hydrate = (componentId) => { // frag/boot.js, pre-fix
    return document.querySelector(~[data-widget="${componentId}"]~)
      ?? document.querySelector(~[data-component="${componentId}"]~);
  };

  // Synthetic positives (never in this repo): different names, same shape.
  function glossaryFind(term) {
    return document.querySelectorAll('dt[data-gloss="' + term + '"]');
  }
  function badgeFor(chip) {
    return chip.closest('[data-chip-badge="' + chip.dataset.badge + '"]');
  }

  // Fixed spellings (e936f791): must stay quiet.
  function pairForFixed(id) {
    const sel = CSS.escape(id);
    return document.querySelector('input[data-fui-range-slider="' + sel + '"].ui-range-slider__input--min');
  }
  function wireFieldFixed(form, whenName) {
    return form.querySelectorAll('[name="' + CSS.escape(whenName) + '"]');
  }
  const hydrateFixed = (name) => {
    return document.querySelector(~[data-widget="${CSS.escape(name)}"]~);
  };

  // Postures that stay silent: module-local cssEscape shim, const string
  // literal, for-of over literal array, bare variable, getElementById.
  const cssEscape = (s) => window.CSS ? CSS.escape(s) : s;
  function shimmed(anchorId) {
    return document.querySelector('#' + cssEscape(anchorId));
  }
  const IS_OPEN = 'data-fui-dropdown-open';
  document.querySelectorAll('[' + IS_OPEN + ']');
  for (const ev of ['input', 'change']) {
    document.querySelector(~[data-action-type="${ev}"]~);
  }
  const prebuilt = '[data-x="y"]';
  document.querySelector(prebuilt);
  document.getElementById('plain');
})();
`

var selectorFixture = strings.ReplaceAll(selectorFixtureRaw, "~", "\x60")

func TestLintSelectorInterpolation_FiresOnPreFixShapes(t *testing.T) {
	dir := writeRuntimeFixture(t, "selector.js", selectorFixture)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The twelve unescaped lookups: pairFor (min, out), wireField,
	// labelFor, mirrorFor, deferFor (×2), styleLoaded, hydrate (×2),
	// glossaryFind, badgeFor.
	wantOperands := map[string]bool{
		"id": false, "whenName": false, "cb.id": false, "k": false,
		"name": false, "componentId": false, "term": false, "chip.dataset.badge": false,
	}
	seen := 0
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[selector-interpolation]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
		found := false
		for op := range wantOperands {
			if strings.Contains(v.Message, `"`+op+`"`) {
				wantOperands[op] = true
				found = true
				break
			}
		}
		if !found {
			t.Errorf("finding on a fixed/silent spelling: %s:%d: %s", v.File, v.Line, v.Message)
		}
		seen++
	}
	for op, got := range wantOperands {
		if !got {
			t.Errorf("expected a finding interpolating %q, got none (full result:\n%s)", op, res.Error())
		}
	}
	if seen < 12 {
		t.Errorf("expected at least 12 findings (one per unescaped lookup), got %d:\n%s", seen, res.Error())
	}
}

func TestLintSelectorInterpolation_SyntheticPositives(t *testing.T) {
	// Shape-only positives in a package layout that never existed here:
	// a glossary module and a chip module, neither name in the runtime.
	dir := writeRuntimeFixture(t, "glossary.js", `(() => {
  const lookup = (term) => document.querySelectorAll('dt[data-gloss="' + term + '"]');
  const byId = (el) => el.closest('[data-gloss-group="' + el.dataset.group + '"]');
  return { lookup, byId };
})();
`)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected 2 findings, got %d:\n%s", len(res.Violations), res.Error())
	}
}

func TestLintSelectorInterpolation_RepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	runtimeDir := filepath.Join(repoRoot, "core-ui", "runtime")
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Skipf("runtime dir not present: %v", err)
	}
	res, err := LintSelectorInterpolation(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("runtime JS contains unescaped selector interpolations:\n%s", res.Error())
	}
}

// ── LintRegistryOwnProps ───────────────────────────────────────────────

const registryDeclsFixture = `// Registry declaration forms from the real corpus: boot.js's
// NS.X = NS.X || {}, kernel.js's object-literal properties, and a
// top-level const.
window.__gofastr = window.__gofastr || {};
const G = window.__gofastr;
G._widgetCatalog = G._widgetCatalog || {};
G.kernel = {
  _widgets: {},
  loadedModules: {},
};
const modulePromises = {};
const notARegistry = Object.create(null);
const own = (o, k) => Object.prototype.hasOwnProperty.call(o, k);
const arr = [];
`

const registryReadsFixture = `// Pre-fix reads (real API names) from widgets.js, rpc.js,
// widgetfocus.js; fixed spellings from e936f791; silent postures; and
// two synthetic positives.
const NS = window.__gofastr;

function openWidget(name) {
  const entry = NS._widgetCatalog && NS._widgetCatalog[name];
  return entry;
}
function rpcRefresh(widgetName) {
  const wentry = NS._widgets && NS._widgets[widgetName];
  return wentry;
}
function loadModule(name) {
  if (modulePromises[name]) return modulePromises[name];
}

// Fixed spellings (e936f791): quiet — guard on the same line and on
// the previous line.
function openWidgetFixed(name) {
  const entry = NS._widgetCatalog
    && Object.prototype.hasOwnProperty.call(NS._widgetCatalog, name)
      ? NS._widgetCatalog[name]
      : undefined;
  return entry;
}
function refreshFixed(widgetName) {
  const rentry = NS._widgets
    && Object.prototype.hasOwnProperty.call(NS._widgets, widgetName)
      ? NS._widgets[widgetName]
      : undefined;
  return rentry;
}

// Silent postures: writes, write-through, delete, in-check, literal
// index, composite key, Object.create(null), for-in index.
NS._widgets[name] = { cfg: name };
NS._widgets[name].outsideHandler = 1;
delete NS._widgets[name];
if (name in NS._widgetCatalog) {}
NS._widgets['literal'];
const byKey = name + '\0' + (ctx || '');
NS._widgets[byKey];
notARegistry[name];
for (const k in NS._widgetCatalog) {
  NS._widgetCatalog[k];
}

// Silent posture: the module-local own() helper (the kernel fragment's
// compact spelling of the idiom), declared in the OTHER fixture file —
function loadModuleHelper(name) {
  const lm = NS.kernel.loadedModules;
  if (lm && own(lm, name) && lm[name]) return true;
  if (own(NS.kernel.loadedModules, name) && NS.kernel.loadedModules[name]) return true;
  return false;
}
// Synthetic positives (never in this repo): different domains, same shape.
const glyphIndex = {};
function lookupGlyph(g) { return glyphIndex[g]; }
const zoneMap = {};
function zoneOf(code) { return zoneMap[code] ?? null; }
`

func TestLintRegistryOwnProps_FiresOnPreFixShapes(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"decls.js":   registryDeclsFixture,
		"readers.js": registryReadsFixture,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := LintRegistryOwnProps(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Cross-file: _widgets/_widgetCatalog are declared in decls.js and
	// read in readers.js. Expected: openWidget, rpcRefresh, loadModule
	// (×2 reads on one line), glyphIndex, zoneOf.
	wantRegistries := map[string]int{
		"_widgetCatalog": 1,
		"_widgets":       1,
		"modulePromises": 1,
		"glyphIndex":     1,
		"zoneMap":        1,
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[registry-own-prop]") {
			t.Errorf("unexpected message: %s", v.Message)
			continue
		}
		matched := false
		for reg := range wantRegistries {
			if strings.HasPrefix(v.Message, "[registry-own-prop] "+reg+"[") {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("finding on a fixed/silent spelling: %s:%d: %s", v.File, v.Line, v.Message)
		}
	}
	for reg := range wantRegistries {
		if !strings.Contains(res.Error(), "[registry-own-prop] "+reg+"[") {
			t.Errorf("expected a finding reading registry %q, got none (full result:\n%s)", reg, res.Error())
		}
	}
}

func TestLintRegistryOwnProps_SyntheticPositives(t *testing.T) {
	dir := writeRuntimeFixture(t, "glyph.js", `const glyphIndex = {};
const zoneMap = {};
function lookupGlyph(g) { return glyphIndex[g]; }
function zoneOf(code) { return zoneMap[code] ?? null; }
`)
	res, err := LintRegistryOwnProps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected 2 findings, got %d:\n%s", len(res.Violations), res.Error())
	}
}

func TestLintRegistryOwnProps_RepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	runtimeDir := filepath.Join(repoRoot, "core-ui", "runtime")
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Skipf("runtime dir not present: %v", err)
	}
	res, err := LintRegistryOwnProps(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("runtime JS contains prototype-chain registry reads:\n%s", res.Error())
	}
}

// ── LintResponseMountedAfterOK ─────────────────────────────────────────

const responseFixture = `// Reduced from the pre-fix sortablelist.js conflict-recovery refresh
// (real API shape), plus synthetic positives and the fixed spelling.
function conflictRefresh(dest, crpc, restoreFn) {
  fetch(crpc, { credentials: 'same-origin' })
    .then(function (r) { return r.text(); })
    .then(function (html) {
      dest.innerHTML = html;
      finishConflict();
    })
    .catch(function () { restoreFn(); });
}

// Synthetic positives (never in this repo): one of the runtime's own
// swap helpers and an insertAdjacentHTML mount.
function loadPanel(slot, path) {
  fetch(path).then(r => r.json()).then(d => { swapPane(slot, d.panel); });
}
function loadBadge(el, src) {
  fetch(src).then(r => r.text()).then(t => el.insertAdjacentHTML('beforeend', t));
}

// Fixed spelling (e936f791): quiet.
function conflictRefreshFixed(dest, crpc, restoreFn) {
  fetch(crpc, { credentials: 'same-origin' })
    .then(function (r) {
      if (!r.ok) throw new Error('conflict refresh failed: ' + r.status);
      return r.text();
    })
    .then(function (html) {
      dest.innerHTML = html;
    })
    .catch(function () { restoreFn(); });
}

// Silent postures: gated chain, no mount, no body read, await form.
fetch(src, {}).then(r => { if (!r.ok) throw new Error('x'); return r.text(); }).then(h => { el.innerHTML = h; });
fetch(src, {}).then(r => r.text()).then(t => console.log(t));
fetch(src, {}).then(r => { el.setAttribute('data-state', 'x'); });
async function awaitForm(path) {
  const r = await fetch(path);
  const h = await r.text();
  el.innerHTML = h;
}
`

func TestLintResponseMountedAfterOK_FiresOnPreFixShape(t *testing.T) {
	dir := writeRuntimeFixture(t, "response.js", responseFixture)
	res, err := LintResponseMountedAfterOK(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 3 {
		t.Fatalf("expected exactly 3 findings (conflictRefresh, loadPanel, loadBadge), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[response-mounted-unchecked]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
	}
}

func TestLintResponseMountedAfterOK_RepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	runtimeDir := filepath.Join(repoRoot, "core-ui", "runtime")
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Skipf("runtime dir not present: %v", err)
	}
	res, err := LintResponseMountedAfterOK(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("runtime JS mounts fetched text without an .ok check:\n%s", res.Error())
	}
}

// ── LintAttributePathSegments ──────────────────────────────────────────

const attrPathFixture = `// Reduced from the pre-fix _kilnPost in rpc.js (real API names),
// plus synthetic positives, the fixed spelling, and silent postures.
const _kilnPost = (el, body) => {
  return fetch('/kiln/tool/' + el.getAttribute('data-kiln-tool'), {
    method: 'POST',
    body,
  }).catch(() => {});
};

// Synthetic positives (never in this repo): a src assignment from
// dataset and an XHR open with an attribute variable.
function mountThumb(card) {
  const img = document.createElement('img');
  img.src = '/media/thumb/' + card.dataset.mediaId;
  return img;
}

// Synthetic positive 2: an XHR open with an attribute variable.
function fetchNote(btn) {
  const note = btn.getAttribute('data-note-id');
  const xhr = new XMLHttpRequest();
  xhr.open('GET', '/api/notes/' + note);
  xhr.send();
}

// Fixed spelling (e936f791): quiet — regex gate in the function.
const _kilnPostFixed = (el, body) => {
  const tool = el.getAttribute('data-kiln-tool') || '';
  if (!/^[A-Za-z0-9_-]+$/.test(tool)) return Promise.resolve();
  return fetch('/kiln/tool/' + tool, {
    method: 'POST',
    body,
  }).catch(() => {});
};

// Silent postures: query parameter, encodeURIComponent, SAFE_NAME-style
// gate, allowlist membership gate.
function trackPing(el) {
  fetch('/analytics/ping?src=' + el.getAttribute('data-src'));
}
function safeFetch(el) {
  const slug = el.getAttribute('data-slug');
  if (!/^[a-z0-9-]+$/.test(slug)) return;
  fetch('/wiki/' + slug);
}
const SAFE_NAME = /^[A-Za-z0-9_-]+$/;
function gatedFetch(el) {
  const name = el.getAttribute('data-name');
  if (!SAFE_NAME.test(name)) return;
  fetch('/items/' + name);
}
function manifestFetch(el) {
  const id = el.getAttribute('data-comp');
  const manifest = window.__manifest || {};
  if (!id || !manifest[id] || window.__seen.has(id)) return;
  const s = document.createElement('script');
  s.src = '/__gofastr/widget/' + id + '.js?v=' + manifest[id];
  document.head.appendChild(s);
}
function encodedFetch(el) {
  fetch('/search/' + encodeURIComponent(el.getAttribute('data-q')));
}
`

func TestLintAttributePathSegments_FiresOnPreFixShape(t *testing.T) {
	dir := writeRuntimeFixture(t, "attrpath.js", attrPathFixture)
	res, err := LintAttributePathSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 3 {
		t.Fatalf("expected exactly 3 findings (_kilnPost, mountThumb, fetchNote), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[attr-path-segment]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
	}
}

func TestLintAttributePathSegments_RepoIsClean(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("can't locate repo root: %v", err)
	}
	runtimeDir := filepath.Join(repoRoot, "core-ui", "runtime")
	if _, err := os.Stat(runtimeDir); err != nil {
		t.Skipf("runtime dir not present: %v", err)
	}
	res, err := LintAttributePathSegments(runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("runtime JS joins attribute-borne values into URL paths ungated:\n%s", res.Error())
	}
}

// ── review findings (7-14): named .then handlers, statusText gates,
// template URLs, hoisted guards, long for-in bodies, window.CSS.escape,
// revocable safe identifiers ────────────────────────────────────────────

const namedThenFixture = `// .then continuations passed by bare reference (review finding 7):
// the fetched text is handed to a named function one declaration away,
// so nothing inside the chain span itself carries the mount token.
function mountPanel(el, html) {
  el.innerHTML = html;
}
function refresh(el, src) {
  fetch(src).then(r => r.text()).then(function (t) { mountPanel(el, t); }).catch(() => {});
}
// The handler itself passed by bare reference (function declaration).
function refresh2(el, src) {
  fetch(src).then(r => r.text()).then(bind);
  function bind(t) { mountPanel(el, t); }
}
// Bare reference to a const-arrow declaration.
function refresh3(el, src) {
  fetch(src).then(r => r.text()).then(panel);
  const panel = (t) => { el.insertAdjacentHTML('beforeend', t); };
}
// Control: the inline spelling the lint already catches.
function refreshInline(el, src) {
  fetch(src).then(r => r.text()).then(t => { el.innerHTML = t; });
}
// Silent: a named continuation that never mounts.
function logIt(t) { console.log(t); }
function refreshLog(el, src) {
  fetch(src).then(r => r.text()).then(logIt);
}
// Silent: a named continuation that gates before mounting.
function mountChecked(el, t) {
  if (!t.ok) return;
  el.innerHTML = t.body;
}
function refreshChecked(el, src) {
  fetch(src).then(r => r.json()).then(mountChecked);
}
`

func TestLintResponseMountedAfterOK_NamedThenHandler(t *testing.T) {
	dir := writeRuntimeFixture(t, "named.js", namedThenFixture)
	res, err := LintResponseMountedAfterOK(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 4 {
		t.Fatalf("expected 4 findings (refresh, refresh2, refresh3, refreshInline), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[response-mounted-unchecked]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
	}
}

const statusTextFixture = `// .statusText displayed is not a .status check (review finding 14):
// the gate is a whole-token .ok/.status read in a condition context,
// not the substring ".status" anywhere in the chain.
function load(el, src, note) {
  fetch(src).then(function (r) {
    note.textContent = 'HTTP ' + r.statusText;
    return r.text();
  }).then(function (t) { el.innerHTML = t; });
}
// Control: no status mention at all — fires.
function loadCtl(el, src, note) {
  fetch(src).then(function (r) {
    note.textContent = 'loading';
    return r.text();
  }).then(function (t) { el.innerHTML = t; });
}
// .okButton is not .ok either — token boundary required.
function loadOkButton(el, src, note) {
  fetch(src).then(function (r) {
    note.textContent = r.okButton;
    return r.text();
  }).then(function (t) { el.innerHTML = t; });
}
// Gates that count, all silent: negated .ok, compared .status, .ok in
// an if condition, ternary on .ok.
function loadNeg(el, src) {
  fetch(src).then(r => { if (!r.ok) throw new Error('x'); return r.text(); }).then(t => { el.innerHTML = t; });
}
function loadCmp(el, src) {
  fetch(src).then(r => { if (r.status !== 200) throw new Error('x'); return r.text(); }).then(t => { el.innerHTML = t; });
}
function loadTruthy(el, src) {
  fetch(src).then(r => { if (r.ok) return r.text(); throw new Error('x'); }).then(t => { el.innerHTML = t; });
}
function loadTern(el, src) {
  fetch(src).then(r => r.ok ? r.text() : Promise.reject(new Error('x'))).then(t => { el.innerHTML = t; });
}
`

func TestLintResponseMountedAfterOK_StatusTextIsNotAGate(t *testing.T) {
	dir := writeRuntimeFixture(t, "statustext.js", statusTextFixture)
	res, err := LintResponseMountedAfterOK(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 3 {
		t.Fatalf("expected exactly 3 findings (load, loadCtl, loadOkButton), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[response-mounted-unchecked]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
	}
}

// attrTplFixtureRaw uses ~ for JS backticks; untailed below.
const attrTplFixtureRaw = `// Template-literal URL building (review finding 8): the same
// attribute-borne value joined into the path after a literal chunk
// ending in "/".
const postTpl = (el, body) => {
  return fetch(~/kiln/tool/${el.getAttribute('data-kiln-tool')}~, {
    method: 'POST',
    body,
  }).catch(() => {});
};
// Template via a variable holding the attribute read.
function loadNote(btn) {
  const note = btn.getAttribute('data-note-id');
  fetch(~ /api/notes/${note}~);
}
// Template plus a '+' operand on the same expression.
function mixed(el) {
  const id = el.dataset.rowId;
  fetch(~ /rows/${id}~ + '?full=1');
}
// Silent: the adjacent literal chunk does not end in "/" (query form).
function queryish(el) {
  fetch(~ /find?q=${el.getAttribute('data-q')}~);
}
// Control: the '+' spelling the lint already catches.
const postCat = (el, body) => {
  return fetch('/kiln/tool/' + el.getAttribute('data-kiln-tool'), {
    method: 'POST',
    body,
  }).catch(() => {});
};
`

var attrTplFixture = strings.ReplaceAll(attrTplFixtureRaw, "~", "\x60")

func TestLintAttributePathSegments_TemplateLiterals(t *testing.T) {
	dir := writeRuntimeFixture(t, "tpl.js", attrTplFixture)
	res, err := LintAttributePathSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 4 {
		t.Fatalf("expected exactly 4 findings (postTpl, loadNote, mixed, postCat), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[attr-path-segment]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
	}
}

const registryHoistedFixture = `// Guard hoisted into a compute-once boolean (review finding 9): a
// boolean assigned exactly once from the guard idiom, referenced in the
// condition of the if/ternary that guards the read, counts.
const catalog = {};
const other = {};
function find(name) {
  const known = Object.prototype.hasOwnProperty.call(catalog, name);
  if (!known) return null;
  return catalog[name];
}
function findTern(name) {
  const has = Object.prototype.hasOwnProperty.call(catalog, name);
  return has ? catalog[name] : null;
}
function findInline(name) {
  if (Object.prototype.hasOwnProperty.call(catalog, name)) {
    return catalog[name];
  }
  return null;
}
// The repo's own fixed spelling: guard on the previous line.
function findFixed(name) {
  const entry = Object.prototype.hasOwnProperty.call(catalog, name)
    ? catalog[name]
    : undefined;
  return entry;
}
// A boolean initialized from a guard on a DIFFERENT registry does not
// guard this one, and per-function scoping keeps one function's boolean
// from laundering another's read.
function findMixed(name) {
  const known = Object.prototype.hasOwnProperty.call(other, name);
  if (!known) return null;
  return catalog[name];
}
// Still fires: no guard at all.
function findAny(name) {
  return catalog[name];
}
`

func TestLintRegistryOwnProps_HoistedGuardBoolean(t *testing.T) {
	dir := writeRuntimeFixture(t, "hoisted.js", registryHoistedFixture)
	res, err := LintRegistryOwnProps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected exactly 2 findings (findMixed, findAny), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[registry-own-prop] catalog[") {
			t.Errorf("finding on a fixed/silent spelling: %s:%d: %s", v.File, v.Line, v.Message)
		}
	}
}

const longForInFixture = `// A for-in loop whose body far exceeds any fixed byte window (review
// finding 10): the loop span is brace-matched, so the enumeration-key
// read stays bound however long the body grows.
const reg = {};
for (const k in reg) {
  console.log('padding step 0 with a reasonably long line of ordinary code');
  console.log('padding step 1 with a reasonably long line of ordinary code');
  console.log('padding step 2 with a reasonably long line of ordinary code');
  console.log('padding step 3 with a reasonably long line of ordinary code');
  console.log('padding step 4 with a reasonably long line of ordinary code');
  console.log('padding step 5 with a reasonably long line of ordinary code');
  console.log('padding step 6 with a reasonably long line of ordinary code');
  console.log('padding step 7 with a reasonably long line of ordinary code');
  console.log('padding step 8 with a reasonably long line of ordinary code');
  console.log('padding step 9 with a reasonably long line of ordinary code');
  console.log('padding step 10 with a reasonably long line of ordinary code');
  console.log('padding step 11 with a reasonably long line of ordinary code');
  console.log('padding step 12 with a reasonably long line of ordinary code');
  console.log('padding step 13 with a reasonably long line of ordinary code');
  total += reg[k];
}
// The same identifier OUTSIDE the loop: the binding ends at the brace.
after += reg[k];
`

func TestLintRegistryOwnProps_LongForInBody(t *testing.T) {
	dir := writeRuntimeFixture(t, "longforin.js", longForInFixture)
	res, err := LintRegistryOwnProps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected exactly 1 finding (the read after the loop), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	if !strings.HasPrefix(res.Violations[0].Message, "[registry-own-prop] reg[") {
		t.Errorf("unexpected message: %s", res.Violations[0].Message)
	}
}

const selReassignFixture = `// window.CSS.escape (review finding 11): the defensive global
// reference is the same escape.
function viaWindow(id) {
  return document.querySelector('#' + window.CSS.escape(id));
}
// Reassigned literal (review finding 13): kind is initialized from a
// literal but REASSIGNED from an attribute before use — not provably a
// literal at the lookup.
let kind = 'input';
kind = el.dataset.kind;
document.querySelector('[data-kind="' + kind + '"]');
// Same name, different functions: the escape in viaEsc must not launder
// the attribute-borne sel in viaAttr.
function viaEsc(id) {
  const sel = CSS.escape(id);
  return document.querySelector('[data-x="' + sel + '"]');
}
function viaAttr(el) {
  const sel = el.dataset.sel;
  return document.querySelector('[data-x="' + sel + '"]');
}
// Silent postures that must survive: literal constant, literal read
// with no reassignment, for-of over an array of literals.
const IS_OPEN2 = 'data-fui-open';
document.querySelectorAll('[' + IS_OPEN2 + ']');
function viaLit() {
  const tag = 'section';
  return document.querySelector(tag + '[data-live]');
}
for (const ev of ['input', 'change']) {
  document.querySelector('[data-action-type="' + ev + '"]');
}
`

func TestLintSelectorInterpolation_WindowEscapeAndReassignedSafeIdents(t *testing.T) {
	dir := writeRuntimeFixture(t, "reassign.js", selReassignFixture)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected exactly 2 findings (the reassigned kind, viaAttr's sel), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.Contains(v.Message, `"kind"`) && !strings.Contains(v.Message, `"sel"`) {
			t.Errorf("finding on a fixed/silent spelling: %s:%d: %s", v.File, v.Line, v.Message)
		}
	}
}

// ── review 5 findings: revoked escape results, regex delimiters,
// numeric interpolation, string-literal code, inline registry decls,
// attribute indices, the in operator, compound mounts, optional
// chaining, unanchored and late gates, numeric coercion, fragments ──

// selRevTwoFixtureRaw uses ~ for JS backticks; untailed below.
const selRevTwoFixtureRaw = `// An escape result revoked by a later attribute reassignment
// (review 5): the safe set follows assignments, not spellings.
function findTarget(el) {
  let id = CSS.escape('fixed');
  id = el.dataset.target;
  return document.querySelector('[data-target="' + id + '"]');
}
// A regex literal carrying an unbalanced delimiter inside a selector
// call must not truncate the call span (review 5): the closing paren
// of selectorPrefix(/[}]/) is not the closing paren of querySelector.
function viaPrefix(el) {
  return document.querySelector(selectorPrefix(/[}]/) + el.dataset.target);
}
function selectorPrefix(pattern) { return '#'; }
// Arithmetic on a numeric literal cannot carry selector
// metacharacters (review 5): nth-child(index + 1) is quiet.
function rows(list) {
  const found = [];
  for (let index = 0; index < 3; index++) {
    found.push(list.querySelector(~li:nth-child(${index + 1})~));
  }
  return found;
}
`

var selRevTwoFixture = strings.ReplaceAll(selRevTwoFixtureRaw, "~", "\x60")

func TestLintSelectorInterpolation_Review5SpansAndArithmetic(t *testing.T) {
	dir := writeRuntimeFixture(t, "review5sel.js", selRevTwoFixture)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected exactly 2 findings (the revoked id, the regex-delim span), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	if !strings.Contains(res.Violations[0].Message, `"id"`) &&
		!strings.Contains(res.Violations[1].Message, `"id"`) {
		t.Errorf("expected one finding on the revoked id: %s", res.Error())
	}
	if !strings.Contains(res.Error(), "selectorPrefix") {
		t.Errorf("expected a finding in the span past the regex delimiter: %s", res.Error())
	}
}

// Code-shaped text inside string literals is prose, not a call (review
// 5): both examples must stay quiet under the selector and attribute
// lints.
const stringCodeFixture = `const selectorExample = ~document.querySelector('[data-target="' + el.dataset.target + '"]')~;
const fetchExample = ~fetch('/kiln/tool/' + el.dataset.tool)~;
console.log(selectorExample, fetchExample);
`

var stringCodeFixtureJS = strings.ReplaceAll(stringCodeFixture, "~", "\x60")

func TestLintSelectorAndAttribute_StringLiteralCodeIsQuiet(t *testing.T) {
	dir := writeRuntimeFixture(t, "stringcode.js", stringCodeFixtureJS)
	sel, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if sel.HasErrors() {
		t.Errorf("selector lint fired on string-literal prose:\n%s", sel.Error())
	}
	attr, err := LintAttributePathSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if attr.HasErrors() {
		t.Errorf("attribute lint fired on string-literal prose:\n%s", attr.Error())
	}
}

const registryReview5Fixture = `// Review 5: declarations mid-line (a one-line function or minified
// source declares its {} registry like any other), attribute-borne
// member indices, and the in operator.
function lookupInline(name){const REGISTRY={};return REGISTRY[name]}
const REGISTRY2 = {};
function lookupMember(el) {
  return REGISTRY2[el.dataset.name];
}
// The in operator walks the prototype chain — it is the bug this lint
// exists for, not a guard against it, so this read reports.
function lookupIn(name) {
  if (name in REGISTRY2) return REGISTRY2[name];
}
// A correctly dominating own-property guard spread over several lines
// by the formatter stays quiet.
function lookupMultiline(name) {
  if (
    Object.prototype.hasOwnProperty.call(
      REGISTRY2,
      name,
    )
  ) {
    return REGISTRY2[name];
  }
}
`

func TestLintRegistryOwnProps_Review5DeclsIndicesAndIn(t *testing.T) {
	dir := writeRuntimeFixture(t, "review5reg.js", registryReview5Fixture)
	res, err := LintRegistryOwnProps(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 3 {
		t.Fatalf("expected exactly 3 findings (inline decl, member index, in-guarded read), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[registry-own-prop] REGISTRY") {
			t.Errorf("finding on a fixed/silent spelling: %s:%d: %s", v.File, v.Line, v.Message)
		}
	}
}

const responseReview5Fixture = `// Review 5: every compound assignment to innerHTML/outerHTML is a
// mount, ?.then is a chain step, and the mount-helper list is
// explicit — swapCase is a string transform, not a mount.
function plusMount(target, src) {
  return fetch(src).then(r => r.text()).then(html => { target.innerHTML += html; });
}
function optionalChain(target, src) {
  return fetch(src)
    ?.then(r => r.text())
    ?.then(html => { target.innerHTML = html; });
}
function loadWord(src) {
  return fetch(src).then(r => r.text()).then(text => swapCase(text));
}
function swapCase(text) {
  return [...text].map(char => char === char.toUpperCase() ? char.toLowerCase() : char.toUpperCase()).join('');
}
// The runtime's own swap helpers (swapPane/swapAtSlot/swapShell) are
// mounts.
function loadSlot(slot, src) {
  return fetch(src).then(r => r.json()).then(d => { swapPane(slot, d.panel); });
}
`

func TestLintResponseMountedAfterOK_Review5MountsAndChains(t *testing.T) {
	dir := writeRuntimeFixture(t, "review5resp.js", responseReview5Fixture)
	res, err := LintResponseMountedAfterOK(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 3 {
		t.Fatalf("expected exactly 3 findings (+=, ?.then, swapPane), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[response-mounted-unchecked]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
	}
}

// attrReview5FixtureRaw uses ~ for JS backticks; untailed below.
const attrReview5FixtureRaw = `// Review 5: bracket dataset reads, unanchored gates, gates after the
// construction, numeric coercion, and fragment hrefs.
function loadTool(el) {
  return fetch(~/kiln/tool/${el.dataset.tool}~);
}
function loadToolBracket(el) {
  return fetch('/kiln/tool/' + el.dataset['tool']);
}
// /./ accepts ../admin — an unanchored gate is no gate.
function loadDot(el) {
  const tool = el.dataset.tool;
  if (/./.test(tool)) {
    return fetch('/kiln/tool/' + tool);
  }
}
// The validating regex runs AFTER the request was built: too late.
function loadLate(el) {
  const tool = el.dataset.tool;
  const request = fetch('/kiln/tool/' + tool);
  if (!/^[A-Za-z0-9_-]+$/.test(tool)) return;
  return request;
}
// Number() output is a number or NaN: never a traversal segment.
function loadItem(el) {
  return fetch('/items/' + Number(el.dataset.itemId));
}
// A '#'-prefixed literal sets a fragment: no request path is built.
function setRoute(link, el) {
  link.href = '#/settings/' + el.dataset.tab;
}
`

var attrReview5Fixture = strings.ReplaceAll(attrReview5FixtureRaw, "~", "\x60")

func TestLintAttributePathSegments_Review5GatesAndSanitizers(t *testing.T) {
	dir := writeRuntimeFixture(t, "review5attr.js", attrReview5Fixture)
	res, err := LintAttributePathSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 4 {
		t.Fatalf("expected exactly 4 findings (template, bracket, unanchored gate, late gate), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[attr-path-segment]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
		if strings.Contains(v.Message, "Number(") || strings.Contains(v.Message, "'#/settings/'") {
			t.Errorf("finding on a sanitized or fragment-only use: %s", v.Message)
		}
	}
}

// ── review 6 findings: status truthiness gates, non-dominating path
// gates, partial escape operands, unproven numeric identifiers,
// separator-matching regex escapes, member assignments and parameter
// shadowing in safe-identifier tracking, regex delimiters closing
// function scopes early ─────────────────────────────────────────────

const responseReview6Fixture = `// Review 6: a bare .status truthiness gate passes for 404 and 500,
// and < 400 admits every redirect — only .ok or a comparison that
// requires a 2xx response is a gate. A bound of 399 is the same leak
// in <= clothing, alone, mirrored, or conjoined with a >= 200 lower
// bound.
function mountStatusTruthy(target, src) {
  return fetch(src).then(r => { if (r.status) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatusWeakLt400(target, src) {
  return fetch(src).then(r => { if (r.status < 400) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatusLe399(target, src) {
  return fetch(src).then(r => { if (r.status <= 399) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatusMirror399(target, src) {
  return fetch(src).then(r => { if (399 >= r.status) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatusWeakRangeLt400(target, src) {
  return fetch(src).then(r => { if (r.status >= 200 && r.status < 400) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatusWeakRangeLe399(target, src) {
  return fetch(src).then(r => { if (r.status >= 200 && r.status <= 399) return r.text(); }).then(html => { target.innerHTML = html; });
}
// Valid gates stay quiet: .ok truthiness (negated or not) and 2xx-only
// status comparisons.
function mountOkChecked(target, src) {
  return fetch(src).then(r => { if (r.ok) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatusEq200(target, src) {
  return fetch(src).then(r => { if (r.status === 200) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatusLt300(target, src) {
  return fetch(src).then(r => { if (r.status < 300) return r.text(); }).then(html => { target.innerHTML = html; });
}
function mountStatus2xxRange(target, src) {
  return fetch(src).then(r => { if (r.status >= 200 && r.status < 300) return r.text(); }).then(html => { target.innerHTML = html; });
}
`

func TestLintResponseMountedAfterOK_Review6StatusGates(t *testing.T) {
	dir := writeRuntimeFixture(t, "review6resp.js", responseReview6Fixture)
	res, err := LintResponseMountedAfterOK(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 6 {
		t.Fatalf("expected exactly 6 findings (truthiness, < 400, <= 399, mirrored 399 >=, range < 400, range <= 399), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	for _, v := range res.Violations {
		if !strings.HasPrefix(v.Message, "[response-mounted-unchecked]") {
			t.Errorf("unexpected message: %s", v.Message)
		}
	}
}

const attrReview6Fixture = `// Review 6: a matching validation in an unrelated earlier branch
// constrains nothing — the branch can be skipped and the URL still
// built — and a gate branch that does not reject (return/throw) only
// sets a flag.
function loadUnrelatedBranch(el, other) {
  const tool = el.getAttribute('data-tool');
  if (other) {
    if (!/^[A-Za-z0-9_-]+$/.test(tool)) { skipTool(tool); }
  }
  return fetch('/kiln/tool/' + tool);
}
function skipTool(t) {}
function loadFlagOnly(el) {
  const tool = el.getAttribute('data-tool');
  if (/^[A-Za-z0-9_-]+$/.test(tool)) { flagTool(); }
  return fetch('/kiln/tool/' + tool);
}
function flagTool() {}
// Dominating gates stay quiet: reject on failure, or enclose the
// construction in the branch where the gate held.
function loadRejectReturn(el) {
  const tool = el.getAttribute('data-tool');
  if (!/^[A-Za-z0-9_-]+$/.test(tool)) return;
  return fetch('/kiln/tool/' + tool);
}
function loadRejectThrow(el) {
  const tool = el.getAttribute('data-tool');
  if (!/^[A-Za-z0-9_-]+$/.test(tool)) throw new Error('bad tool');
  return fetch('/kiln/tool/' + tool);
}
function loadEnclosingBranch(el) {
  const tool = el.getAttribute('data-tool');
  if (/^[A-Za-z0-9_-]+$/.test(tool)) {
    return fetch('/kiln/tool/' + tool);
  }
}
`

func TestLintAttributePathSegments_Review6GateDominance(t *testing.T) {
	dir := writeRuntimeFixture(t, "review6attr.js", attrReview6Fixture)
	res, err := LintAttributePathSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected exactly 2 findings (unrelated branch, flag-only gate), got %d:\n%s",
			len(res.Violations), res.Error())
	}
}

const selReview6FixtureRaw = `// Review 6: an escape call with a trailing || or ?: operand still
// carries the raw attribute value whenever the escape result is falsy
// (the empty string) — the operand must be the WHOLE escape call.
function escapeOrTrailing(el) {
  return document.querySelector('[data-or="' + CSS.escape(el.dataset.v) || el.dataset.v + '"]');
}
function escapeTernaryTrailing(el) {
  return document.querySelector('[data-q="' + CSS.escape(el.dataset.q) ? 'x' : el.dataset.q + '"]');
}
`

var selReview6Fixture = strings.ReplaceAll(selReview6FixtureRaw, "~", "\x60")

func TestLintSelectorInterpolation_Review6PartialEscapeOperands(t *testing.T) {
	dir := writeRuntimeFixture(t, "review6sel.js", selReview6Fixture)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected exactly 2 findings (trailing ||, trailing ?:), got %d:\n%s",
			len(res.Violations), res.Error())
	}
}

// numericReview6FixtureRaw uses ~ for JS backticks; untailed below.
const numericReview6FixtureRaw = `// Review 6: an identifier plus a number is numeric arithmetic only
// when the identifier is PROVEN numeric — from a DOM attribute it is a
// string, and idx + 1 is string concatenation into an nth-child.
function rowFromAttr(el) {
  const idx = el.dataset.rowIndex;
  return document.querySelector(~ul li:nth-child(${idx + 1})~);
}
function rowRevoked(el) {
  let n = 0;
  n = el.dataset.rowIndex;
  return document.querySelector(~ul li:nth-child(${n + 1})~);
}
// Proven numeric: a numeric-literal init and a for-loop counter stay
// quiet.
function rowFromCounter(list) {
  const found = [];
  for (let i = 0; i < 10; i++) {
    found.push(list.querySelector(~li:nth-child(${i + 1})~));
  }
  return found;
}
function rowFromInit() {
  const base = 3;
  return document.querySelector(~li:nth-child(${base + 1})~);
}
`

var numericReview6Fixture = strings.ReplaceAll(numericReview6FixtureRaw, "~", "\x60")

func TestLintSelectorInterpolation_Review6NumericIdentifiersMustBeProven(t *testing.T) {
	dir := writeRuntimeFixture(t, "review6num.js", numericReview6Fixture)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 2 {
		t.Fatalf("expected exactly 2 findings (attribute-borne idx, revoked n), got %d:\n%s",
			len(res.Violations), res.Error())
	}
}

const regexEscapeReview6Fixture = `// Review 6: \x2f, \u002f, \/ and \\ in a "name-safe" gate match a
// path separator or an arbitrary character — the gate accepts foo/bar.
// The m flag is the same leak by other means: ^ and $ go line-bound,
// so "ok\n../admin" passes the gate.
function loadHexSlash(el) {
  const tool = el.getAttribute('data-tool');
  if (/^[A-Za-z0-9_\x2f-]+$/.test(tool)) {
    return fetch('/kiln/tool/' + tool);
  }
}
function loadUniSlash(el) {
  const tool = el.getAttribute('data-tool');
  if (/^[A-Za-z0-9_\u002f-]+$/.test(tool)) {
    return fetch('/kiln/tool/' + tool);
  }
}
function loadEscSlash(el) {
  const tool = el.getAttribute('data-tool');
  if (/^[A-Za-z0-9_\/]+$/.test(tool)) {
    return fetch('/kiln/tool/' + tool);
  }
}
function loadBackslash(el) {
  const tool = el.getAttribute('data-tool');
  if (/^[A-Za-z0-9\\-]+$/.test(tool)) {
    return fetch('/kiln/tool/' + tool);
  }
}
function loadMultiline(el) {
  const tool = el.getAttribute('data-tool');
  if (/^[A-Za-z0-9_-]+$/m.test(tool)) {
    return fetch('/kiln/tool/' + tool);
  }
}
`

func TestLintAttributePathSegments_Review6SeparatorMatchingEscapes(t *testing.T) {
	dir := writeRuntimeFixture(t, "review6re.js", regexEscapeReview6Fixture)
	res, err := LintAttributePathSegments(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 5 {
		t.Fatalf("expected exactly 5 findings (\\x2f, \\u002f, \\/, \\\\, m flag), got %d:\n%s",
			len(res.Violations), res.Error())
	}
}

const safeIdentReview6Fixture = `// Review 6: a member assignment is not an identifier assignment, and
// a function parameter shadows a file-scope safe name inside that
// function. memberOnly's bare id sits in a scope that binds no id and
// precedes the const, so it fires today; were holder.id = 'fixed'
// recorded as a file-scope safe event (the guard this pins), it would
// launder that use instead.
const holder = {};
holder.id = 'fixed';
function memberOnly() {
  return document.querySelector('[data-member="' + id + '"]');
}
function usePlain(id) {
  return document.querySelector('[data-plain="' + id + '"]');
}
const id = 'fixed';
function useParam(id) {
  return document.querySelector('[data-param="' + id + '"]');
}
// The file-scope safe name still covers unshadowed uses, and a local
// literal init still counts.
function usesGlobal() {
  return document.querySelector('[data-global="' + id + '"]');
}
function localLiteral() {
  const kind = 'note';
  return document.querySelector('[data-kind="' + kind + '"]');
}
`

func TestLintSelectorInterpolation_Review6MemberAssignAndParamShadowing(t *testing.T) {
	dir := writeRuntimeFixture(t, "review6safe.js", safeIdentReview6Fixture)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 3 {
		t.Fatalf("expected exactly 3 findings (unbound id before the const, plain parameter id, shadowed parameter id), got %d:\n%s",
			len(res.Violations), res.Error())
	}
}

const regexScopeReview6Fixture = `// Review 6: a '}' inside a regex literal must not close the function
// scope — after /[}]/, a parameter named like a file-scope safe
// identifier still shadows it.
const id = 'fixed';
function afterRegex(id) {
  const open = /[}]/;
  return document.querySelector('[data-after="' + id + '"]');
}
function plainAfterRegex() {
  const open = /[}]/;
  return document.querySelector('[data-plain2="' + id + '"]');
}
`

func TestLintSelectorInterpolation_Review6RegexLiteralMustNotCloseScopes(t *testing.T) {
	dir := writeRuntimeFixture(t, "review6scope.js", regexScopeReview6Fixture)
	res, err := LintSelectorInterpolation(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Violations) != 1 {
		t.Fatalf("expected exactly 1 finding (the shadowed parameter after the regex), got %d:\n%s",
			len(res.Violations), res.Error())
	}
	if !strings.Contains(res.Violations[0].Message, `"id"`) {
		t.Errorf("expected the finding on the shadowed id: %s", res.Error())
	}
}
