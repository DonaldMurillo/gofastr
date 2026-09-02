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

// Synthetic positives (never in this repo): a swap helper and an
// insertAdjacentHTML mount.
function loadPanel(slot, path) {
  fetch(path).then(r => r.json()).then(d => { swap(slot, d.panel); });
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
