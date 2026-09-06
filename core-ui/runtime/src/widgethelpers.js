// Declarative helpers used by widget chrome and ordinary page forms. Kept
// separate from widgets.js so a basic modal does not pay for every optional
// form behavior. The runtime loads this module only when a marker appears.
(function () {
  'use strict';
  const G = window.__gofastr;
  const persistWired = new WeakSet();
  const countWired = new WeakSet();
  const clearWired = new WeakSet();
  const enterWired = new WeakSet();
  let ticking = false;

  const validityWired = new WeakSet();
  // Persist keys are namespaced and component-encoded (the banner.js
  // dismissKey shape): "gofastr.persist." + encodeURIComponent(key). An
  // attribute-borne data-fui-persist-storage value can therefore only ever
  // name an entry inside this module's own namespace, never another
  // feature's localStorage key; a value stored under the pre-namespace raw
  // spelling is not read.
  const PERSIST_PREFIX = 'gofastr.persist.';

  document.addEventListener('click', function (e) {
    const btn = e.target.closest && e.target.closest('[data-fui-fill-input]');
    if (!btn) return;
    const sel = btn.getAttribute('data-fui-fill-input');
    const widget = btn.closest('[data-fui-widget]');
    // data-fui-fill-input is a selector by design: a malformed value
    // degrades to a no-op instead of throwing out of the delegated click
    // handler before its preventDefault.
    let target = null;
    try {
      target = sel && ((widget && widget.querySelector(sel)) || document.querySelector(sel));
    } catch (_) { target = null; }
    if (!target) return;
    e.preventDefault();
    const explicit = btn.getAttribute('data-fui-fill-text');
    target.value = explicit !== null ? explicit : btn.textContent.trim();
    target.dispatchEvent(new Event('input', { bubbles: true }));
    try { target.focus(); target.select?.(); } catch (_) {}
  });

  function startTicker() {
    if (ticking) return;
    ticking = true;
    const tick = function () {
      document.querySelectorAll('[data-fui-tick-elapsed]').forEach(function (el) {
        const start = parseInt(el.getAttribute('data-fui-tick-elapsed'), 10);
        if (!start) return;
        const ms = Date.now() - start;
        el.textContent = ms < 1000 ? ms + 'ms' : ms < 10000 ? (ms / 1000).toFixed(1) + 's' : Math.round(ms / 1000) + 's';
      });
    };
    tick();
    setInterval(tick, 200);
  }

  function wirePersist(el) {
    if (persistWired.has(el)) return;
    persistWired.add(el);
    const key = el.getAttribute('data-fui-persist-storage');
    if (!key) return;
    try {
      // Namespaced and component-encoded at every sink; a draft stored
      // under the pre-namespace raw spelling is not read.
      const saved = localStorage.getItem(PERSIST_PREFIX + encodeURIComponent(key));
      if (saved && !el.value) {
        el.value = saved;
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
    } catch (_) {}
    el.addEventListener('input', function () {
      // Guard spelled at the sink (see PERSIST_PREFIX above).
      try { localStorage.setItem(PERSIST_PREFIX + encodeURIComponent(key), el.value); } catch (_) {}
    });
    if (el.form) el.form.addEventListener('reset', function () {
      try { localStorage.removeItem(PERSIST_PREFIX + encodeURIComponent(key)); } catch (_) {}
    });
  }

  function wireCount(el) {
    if (countWired.has(el)) return;
    countWired.add(el);
    const sel = el.getAttribute('data-fui-charcount-source');
    // Selector by design (see fill-input): malformed degrades to a no-op.
    let src = null;
    try { src = sel && document.querySelector(sel); } catch (_) { src = null; }
    if (!src) return;
    const sync = function () { el.textContent = src.value.length + ' chars'; };
    src.addEventListener('input', sync);
    if (src.form) src.form.addEventListener('reset', function () { requestAnimationFrame(sync); });
    sync();
  }

  function wireClear(el) {
    if (clearWired.has(el)) return;
    clearWired.add(el);
    el.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape' || !el.value) return;
      e.preventDefault();
      e.stopPropagation();
      el.value = '';
      el.dispatchEvent(new Event('input', { bubbles: true }));
    });
  }

  function wireEnter(form) {
    if (enterWired.has(form)) return;
    enterWired.add(form);
    const isEnter = function (e) { return e.key === 'Enter' || e.code === 'Enter' || e.keyCode === 13; };
    form.querySelectorAll('textarea').forEach(function (ta) {
      ta.addEventListener('keydown', function (e) {
        if (!isEnter(e) || e.shiftKey || e.isComposing) return;
        e.preventDefault();
        e.stopPropagation();
        if (form.requestSubmit) form.requestSubmit();
        else form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
      });
      ta.addEventListener('keypress', function (e) {
        if (!isEnter(e) || e.shiftKey) return;
        e.preventDefault();
        e.stopPropagation();
      });
    });
  }

  function wireValidity(form) {
    if (validityWired.has(form)) return;
    validityWired.add(form);
    const sync = function () {
      const disabled = !form.checkValidity();
      form.querySelectorAll('button[type="submit"],input[type="submit"]').forEach(function (btn) { btn.disabled = disabled; });
    };
    form.addEventListener('input', sync);
    form.addEventListener('change', sync);
    form.addEventListener('reset', function () { requestAnimationFrame(sync); });
    Promise.resolve().then(sync);
  }

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    if (scope.querySelector('[data-fui-tick-elapsed]')) startTicker();
    scope.querySelectorAll('[data-fui-persist-storage]').forEach(wirePersist);
    scope.querySelectorAll('[data-fui-charcount-source]').forEach(wireCount);
    scope.querySelectorAll('[data-fui-clear-on-esc]').forEach(wireClear);
    scope.querySelectorAll('form[data-fui-submit-on-enter]').forEach(wireEnter);
    scope.querySelectorAll('form[data-fui-disable-when-invalid]').forEach(wireValidity);
  }

  scan(document);
  // Register BOTH the scanner and the loaded flag: the runtime's
  // MutationObserver / gofastr:navigate rescan loops only invoke
  // scanners of modules marked loaded, and a remounted widget (close →
  // reopen builds fresh DOM) or a poll-swapped tool row arrives through
  // exactly those loops.
  (G._moduleScanners ||= {}).widgethelpers = scan;
  (G.loadedModules ||= {}).widgethelpers = true;
})();
