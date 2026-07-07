// GoFastr runtime module — Toasts
//
// Toast stack runtime:
//   - __gofastr.toast(cfg|string) JS API to fire toasts at runtime.
//   - Auto-dismiss via data-fui-toast-ttl-ms with hover/focus pause.
//   - Click-to-dismiss via [data-fui-toast-dismiss] inside an item.
//   - Wires existing [data-fui-toast-id] items inside any
//     [data-fui-toast-stack] container (SSR-inlined stacks).
//
// Loads on demand:
//   - core's marker scanner watches [data-fui-toast-stack] and
//     [data-fui-toast] (click triggers) and idle-loads this module.
//   - core's X-Gofastr-Toast header dispatcher awaits
//     loadModule('toasts') before firing.
//   - hover/focus prefetch via data-fui-prefetch="toasts" warms it.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  NS._toastTimers = NS._toastTimers || {};
  NS._toastSeq = NS._toastSeq || 0;

  /** Wire freshly-swapped toast items inside `root`: schedule auto
      dismiss via data-fui-toast-ttl-ms, pause on hover/focus, resume
      on leave, click-to-dismiss via [data-fui-toast-dismiss]. The
      same toast HTML re-rendered should NOT reset existing timers
      — we key by toast id and skip already-known ones. */
  NS._initToasts = function (root) {
    const items = root.querySelectorAll('[data-fui-toast-id]');
    // Track which ids are present in the current DOM. Anything in
    // _toastTimers but absent here was dismissed — cancel its timer.
    const present = {};
    items.forEach((item) => {
      const id = item.getAttribute('data-fui-toast-id');
      present[id] = true;
      if (NS._toastTimers[id]) return; // already wired
      const ttl = parseInt(item.getAttribute('data-fui-toast-ttl-ms') || '0', 10);
      if (ttl > 0) {
        const rec = { remaining: ttl, startedAt: Date.now(), timer: 0 };
        NS._toastTimers[id] = rec;
        const arm = () => {
          rec.startedAt = Date.now();
          rec.timer = setTimeout(() => NS._dismissToast(item, id), rec.remaining);
        };
        const pause = () => {
          if (!rec.timer) return;
          clearTimeout(rec.timer);
          rec.timer = 0;
          rec.remaining -= Date.now() - rec.startedAt;
          if (rec.remaining < 100) rec.remaining = 100;
        };
        item.addEventListener('mouseenter', pause);
        item.addEventListener('focusin', pause);
        item.addEventListener('mouseleave', arm);
        item.addEventListener('focusout', arm);
        arm();
      }
      item.addEventListener('click', (e) => {
        if (e.target.closest('[data-fui-toast-dismiss]')) {
          e.preventDefault();
          NS._dismissToast(item, id);
        }
      });
    });
    // Cancel timers for toasts that are no longer in the DOM
    // (server-side dismissal / replacement).
    for (const id in NS._toastTimers) {
      if (!present[id]) {
        clearTimeout(NS._toastTimers[id].timer);
        delete NS._toastTimers[id];
      }
    }
  };

  /** Add the leave class, remove from DOM after the CSS animation
      finishes. Idempotent: a second dismiss on a leaving toast is
      a no-op. */
  NS._dismissToast = function (item, id) {
    if (!item || item.classList.contains('is-leaving')) return;
    item.classList.add('is-leaving');
    const rec = NS._toastTimers[id];
    if (rec) { clearTimeout(rec.timer); delete NS._toastTimers[id]; }
    const cs = getComputedStyle(item);
    const ms = parseFloat(cs.animationDuration) * 1000 || 200;
    setTimeout(() => { if (item.parentNode) item.parentNode.removeChild(item); }, ms);
  };

  /** Push a toast onto a stack. cfg = { variant, title, body, ttl, stack }
      OR a bare string treated as { title: <string>, ttl: 4000 } for
      ergonomic ad-hoc calls.

      Returns the new item's id. */
  NS.toast = function (cfg) {
    if (cfg == null) return null;
    if (typeof cfg === 'string') cfg = { title: cfg, ttl: 4000 };
    if (!cfg.title) return null;
    let container = null;
    if (cfg.stack) {
      container = document.querySelector('[data-fui-toast-stack="' + CSS.escape(cfg.stack) + '"]');
    }
    if (!container) {
      container = document.querySelector('[data-fui-toast-stack]');
    }
    if (!container) {
      // Body singleton (doc.MANIFEST: fui-toast-stack-auto) — created at
      // most once and re-attached by the SPA full-shell swap. Distinct
      // from core's unstyled fui-toast-fallback container, which exists
      // only for the "toasts module failed to load" path.
      container = NS.doc.singleton('fui-toast-stack-auto', () => {
        const c = document.createElement('div');
        c.className = 'ui-toast-stack';
        c.setAttribute('data-fui-comp', 'ui-toast-stack');
        c.setAttribute('data-fui-toast-stack', '__auto');
        c.style.cssText = 'position:fixed;top:1rem;right:1rem;z-index:2147483600;display:grid;gap:0.5rem;pointer-events:none;max-width:min(360px,calc(100vw - 2rem));';
        return c;
      });
      if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(container);
    }

    const id = 't' + (++NS._toastSeq);
    const variant = cfg.variant || 'info';
    const isAssertive = variant === 'warning' || variant === 'danger';
    const role = isAssertive ? 'alert' : 'status';
    const live = isAssertive ? 'assertive' : 'polite';
    const glyph = ({ success: '✓', warning: '!', danger: '✕', info: 'i', neutral: '•' })[variant] || 'i';

    const ttl = parseInt(cfg.ttl || 0, 10);
    const item = document.createElement('div');
    item.className = 'ui-toast-stack__item';
    item.setAttribute('data-fui-toast-id', id);
    if (ttl > 0) item.setAttribute('data-fui-toast-ttl-ms', String(ttl));

    const wrap = document.createElement('div');
    wrap.className = 'ui-notification ui-notification--' + variant;
    wrap.setAttribute('data-fui-comp', 'ui-notification');
    wrap.setAttribute('role', role);
    wrap.setAttribute('aria-live', live);

    const icon = document.createElement('span');
    icon.className = 'ui-notification__icon';
    icon.setAttribute('aria-hidden', 'true');
    icon.textContent = glyph;

    const text = document.createElement('div');
    text.className = 'ui-notification__text';
    const titleEl = document.createElement('strong');
    titleEl.className = 'ui-notification__title';
    titleEl.textContent = cfg.title;
    text.appendChild(titleEl);
    if (cfg.body) {
      const bodyEl = document.createElement('p');
      bodyEl.className = 'ui-notification__body';
      bodyEl.textContent = cfg.body;
      text.appendChild(bodyEl);
    }

    const dismiss = document.createElement('button');
    dismiss.type = 'button';
    dismiss.className = 'ui-notification__dismiss';
    dismiss.setAttribute('aria-label', 'Dismiss notification');
    dismiss.setAttribute('data-fui-toast-dismiss', '');
    dismiss.textContent = '×';

    wrap.appendChild(icon);
    wrap.appendChild(text);
    wrap.appendChild(dismiss);
    item.appendChild(wrap);
    container.appendChild(item);

    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(item);
    NS._initToasts(container);
    return id;
  };

  // Wire any SSR-inlined toast stacks that already exist in the DOM.
  // The framework's ToastStack preset renders a container div with
  // [data-fui-toast-stack="<name>"] — when those exist at module-load
  // time we want their items wired immediately. (Empty stacks no-op.)
  const _rescan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    scope.querySelectorAll('[data-fui-toast-stack]').forEach((c) => NS._initToasts(c));
  };
  _rescan(document);

  // Core dispatches `gofastr:navigate` after every SPA-nav swap; loaded
  // modules re-init against the fresh DOM via this scanner registry.
  // Without this, SSR-inlined toast stacks rendered onto the new page
  // would never get their TTL timers armed — _initToasts ran exactly
  // once at module load, before that DOM existed.
  ((NS._moduleScanners ||= {})).toasts = _rescan;

  (NS.loadedModules ||= {}).toasts = true;
})();
