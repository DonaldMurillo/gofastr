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
  const validityWired = new WeakSet();
  let ticking = false;

  document.addEventListener('click', function (e) {
    const btn = e.target.closest && e.target.closest('[data-fui-fill-input]');
    if (!btn) return;
    const sel = btn.getAttribute('data-fui-fill-input');
    const widget = btn.closest('[data-fui-widget]');
    const target = sel && ((widget && widget.querySelector(sel)) || document.querySelector(sel));
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
      const saved = localStorage.getItem(key);
      if (saved && !el.value) {
        el.value = saved;
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
    } catch (_) {}
    el.addEventListener('input', function () {
      try { localStorage.setItem(key, el.value); } catch (_) {}
    });
    if (el.form) el.form.addEventListener('reset', function () {
      try { localStorage.removeItem(key); } catch (_) {}
    });
  }

  function wireCount(el) {
    if (countWired.has(el)) return;
    countWired.add(el);
    const sel = el.getAttribute('data-fui-charcount-source');
    const src = sel && document.querySelector(sel);
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
