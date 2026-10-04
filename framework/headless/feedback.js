// headless-feedback: the behaviour module for the feedback family —
// copy controls, the toast stack runtime, the notification bell's
// spoken count, and the offline banner's retry control. Loaded by the
// kernel on one of its markers, at boot, on insertion, or after a
// client navigation.
//
// The toast runtime is the API the kernel's response-header path and
// every window.__gofastr.toast caller already speak (NS.toast,
// NS._initToasts, NS._dismissToast, NS._toastTimers, NS._toastSeq):
// it moved here from the retired core-ui/runtime "toasts" module, and
// the kernel's loadModule('headless-feedback') retarget is what keeps
// the X-Gofastr-Toast dispatch working. Every sentence the module
// writes arrived as a data-hui-* attribute the component rendered
// from its Strings — the dismiss label and the copy status travel on
// the stack and the copy control — so a translated page announces in
// its own language.
(function () {
  'use strict';
  const NAME = 'headless-feedback';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function within(root, sel) {
    const out = [];
    if (root.matches && root.matches(sel)) out.push(root);
    if (root.querySelectorAll) out.push.apply(out, root.querySelectorAll(sel));
    return out;
  }

  // ─── toast stack runtime (moved from src/toasts.js) ─────────────

  NS._toastTimers = NS._toastTimers || new Map();
  NS._toastSeq = NS._toastSeq || 0;

  NS._initToasts = function (root) {
    // A row the module built carries its id; a row the server
    // rendered carries the toast marker and, when it has a lifetime,
    // the TTL. Both arm here; a server row is given its id on sight.
    const items = root.querySelectorAll('[data-hui-toast-id], [data-hui-toast-ttl-ms]');
    const present = new Set();
    items.forEach(function (item) {
      if (!item.hasAttribute('data-hui-toast-id')) item.setAttribute('data-hui-toast-id', 's' + (++NS._toastSeq));
      const id = item.getAttribute('data-hui-toast-id');
      present.add(id);
      // Ids are attribute-borne, so the registry is a Map: plain
      // string keys, no prototype re-parenting.
      if (NS._toastTimers.get(id)) return;
      const ttl = parseInt(item.getAttribute('data-hui-toast-ttl-ms') || '0', 10);
      if (ttl > 0) {
        const rec = { remaining: ttl, startedAt: Date.now(), timer: 0 };
        NS._toastTimers.set(id, rec);
        const arm = function () {
          rec.startedAt = Date.now();
          rec.timer = setTimeout(function () { NS._dismissToast(item, id); }, rec.remaining);
        };
        const pause = function () {
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
      // A server row's dismiss is a link with its Island: the kernel
      // owns that click. The module's own rows dismiss here.
      if (item.hasAttribute('data-hui-toast')) return;
      item.addEventListener('click', function (e) {
        if (e.target.closest('[data-hui-toast-dismiss]')) {
          e.preventDefault();
          NS._dismissToast(item, id);
        }
      });
    });
    for (const entry of Array.from(NS._toastTimers)) {
      if (!present.has(entry[0])) {
        clearTimeout(entry[1].timer);
        NS._toastTimers.delete(entry[0]);
      }
    }
  };

  NS._dismissToast = function (item, id) {
    if (!item || item.hasAttribute('data-hui-toast-leaving')) return;
    // The leaving mark is the module's own (written here, rendered by
    // no component): the kit's stack sheet animates the row out on it.
    item.setAttribute('data-hui-toast-leaving', '');
    const rec = NS._toastTimers.get(id);
    if (rec) { clearTimeout(rec.timer); NS._toastTimers.delete(id); }
    const cs = getComputedStyle(item);
    const ms = parseFloat(cs.animationDuration) * 1000 || 200;
    setTimeout(function () { if (item.parentNode) item.parentNode.removeChild(item); }, ms);
  };

  // ─── toast rows ─────────────────────────────────────────────────

  // Every word a built row says arrived on the stack or its template
  // from Strings, and every class it wears came from the template the
  // kit registered (registry.RegisterTemplate under preset.ToastTemplate,
  // rendered by preset's slot inside the stack). The module names hooks
  // only.
  const TONES = ['info', 'success', 'warning', 'danger'];
  const TONE_ATTR = {
    info: 'data-hui-toast-tone-info', success: 'data-hui-toast-tone-success',
    warning: 'data-hui-toast-tone-warning', danger: 'data-hui-toast-tone-danger'
  };
  const GLYPH_ATTR = {
    info: 'data-hui-toast-glyph-info', success: 'data-hui-toast-glyph-success',
    warning: 'data-hui-toast-glyph-warning', danger: 'data-hui-toast-glyph-danger'
  };
  const VARIANT_ATTR = {
    info: 'data-hui-toast-variant-info', success: 'data-hui-toast-variant-success',
    warning: 'data-hui-toast-variant-warning', danger: 'data-hui-toast-variant-danger'
  };

  // findStack: the named stack, else the first on the page. None is
  // null: the module mounts no region of its own — a layout mounts
  // one, or framework/uihost mounts the default — and the kernel's
  // fallback region takes the toast on null.
  function findStack(name) {
    let c = null;
    if (name) c = document.querySelector('[data-cui-toast-stack="' + CSS.escape(name) + '"]');
    return c || document.querySelector('[data-cui-toast-stack], [data-hui-toast-stack]');
  }

  // rowTemplate: the stack's own template first, then any on the page.
  function rowTemplate(container) {
    return container.querySelector(':scope > template[data-hui-toast-template]')
      || document.querySelector('template[data-hui-toast-template]');
  }

  // bareRow: the row shape with its hooks and nothing else, for a page
  // that registered no template. No sheet styles it; it is still a row
  // a reader hears and a click dismisses.
  function bareRow() {
    const item = document.createElement('div');
    item.setAttribute('data-hui-toast-item', '');
    const root = document.createElement('div');
    root.setAttribute('data-hui-toast', '');
    ['data-hui-toast-tone', 'data-hui-toast-icon', 'data-hui-toast-title', 'data-hui-toast-body'].forEach(function (hook) {
      const el = document.createElement('span');
      el.setAttribute(hook, '');
      root.appendChild(el);
    });
    const dismiss = document.createElement('button');
    dismiss.type = 'button';
    dismiss.setAttribute('data-hui-toast-dismiss', '');
    dismiss.textContent = '×';
    root.appendChild(dismiss);
    item.appendChild(root);
    return item;
  }

  // fill: the part says its text, or goes when there is none to say,
  // so a cloned row matches what the server renders for the same
  // content.
  function fill(root, hook, text) {
    const el = root.querySelector('[' + CSS.escape(hook) + ']');
    if (!el) return;
    if (text) el.textContent = text;
    else if (el.parentNode) el.parentNode.removeChild(el);
  }

  // readWord: the stack's Strings first, the template's second.
  function readWord(container, tpl, attr) {
    return container.getAttribute(attr) || (tpl && tpl.getAttribute(attr)) || '';
  }

  NS.toast = function (cfg) {
    if (cfg == null) return null;
    if (typeof cfg === 'string') cfg = { title: cfg, ttl: 4000 };
    if (!cfg.title) return null;
    const container = findStack(cfg.stack);
    if (!container) return null;

    const id = 't' + (++NS._toastSeq);
    const variant = cfg.variant || 'info';
    const tone = TONES.indexOf(variant) >= 0 ? variant : 'info';
    const assertive = tone === 'warning' || tone === 'danger';

    const tpl = rowTemplate(container);
    let item = null;
    if (tpl && tpl.content && tpl.content.firstElementChild) {
      const frag = tpl.content.cloneNode(true);
      item = frag.querySelector('[data-hui-toast-item]') || frag.firstElementChild;
    }
    if (!item) item = bareRow();
    item.setAttribute('data-hui-toast-id', id);
    const ttl = parseInt(cfg.ttl || 0, 10);
    if (ttl > 0) item.setAttribute('data-hui-toast-ttl-ms', String(ttl));

    const root = item.querySelector('[data-hui-toast]') || item;
    const variantCls = tpl ? (tpl.getAttribute(VARIANT_ATTR[tone]) || '') : '';
    variantCls.split(/\s+/).forEach(function (c) { if (c) root.classList.add(c); });
    root.setAttribute('role', assertive ? 'alert' : 'status');
    root.setAttribute('aria-live', assertive ? 'assertive' : 'polite');

    const toneWord = readWord(container, tpl, TONE_ATTR[tone]);
    fill(root, 'data-hui-toast-tone', toneWord ? toneWord + ': ' : '');
    fill(root, 'data-hui-toast-icon', tpl ? (tpl.getAttribute(GLYPH_ATTR[tone]) || '') : '');
    fill(root, 'data-hui-toast-title', cfg.title);
    fill(root, 'data-hui-toast-body', cfg.body || '');
    const dismiss = root.querySelector('[data-hui-toast-dismiss]');
    if (dismiss) {
      const fmt = readWord(container, tpl, 'data-hui-toast-dismiss-label') || '%s';
      dismiss.setAttribute('aria-label', fmt.replace('%s', cfg.title));
    }
    container.appendChild(item);

    // The stack's capacity: past it the oldest rows go, so a burst of
    // toasts cannot pile a column over the page. The list is static,
    // so the surplus is counted once and removed oldest-first.
    const max = parseInt(container.getAttribute('data-hui-toast-max') || '0', 10);
    if (max > 0) {
      const rows = container.querySelectorAll('[data-hui-toast-id]');
      for (let i = 0; i < rows.length - max; i++) container.removeChild(rows[i]);
    }

    if (NS.scanAndLoadCSS) NS.scanAndLoadCSS(item);
    NS._initToasts(container);
    return id;
  };

  // ─── copy ────────────────────────────────────────────────────────

  // The target stays readable and selectable; without script the page
  // promises nothing about the clipboard. The words (button label,
  // copied label, status sentence with {name}) travel on the wrapper
  // from Strings.
  document.addEventListener('click', function (e) {
    const t = e.target;
    const wrap = t && t.closest && t.closest('[data-hui-copy]');
    if (!wrap) return;
    const id = wrap.getAttribute('data-hui-copy-target');
    const target = id ? document.getElementById(id) : null;
    if (!target) return;
    const text = (target.innerText || target.textContent || '').trim();
    if (!text) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).catch(function () {});
    }
    wrap.setAttribute('data-hui-copy-state', 'done');
    const btn = wrap.querySelector('[data-hui-copy-label]');
    const fmts = wrap.getAttribute('data-hui-copy-copied') || '';
    if (btn && fmts) btn.textContent = fmts;
    const status = wrap.querySelector('[data-hui-copy-status]');
    if (status) {
      const name = wrap.getAttribute('data-hui-copy-name') || id || '';
      const sentence = (wrap.getAttribute('data-hui-copy-sentence') || '').replace('{name}', name);
      status.textContent = '';
      requestAnimationFrame(function () { status.textContent = sentence; });
    }
    const back = wrap.getAttribute('data-hui-copy-back') || '';
    setTimeout(function () {
      wrap.removeAttribute('data-hui-copy-state');
      if (btn && back) btn.textContent = back;
    }, 1200);
    // A toast on copy rides this module's own toast runtime; the
    // config JSON travels on the button that carries it.
    const toastEl = wrap.querySelector('[data-hui-copy-toast]');
    const toastCfg = (toastEl && toastEl.getAttribute('data-hui-copy-toast')) || '';
    if (toastCfg) {
      try { NS.toast(JSON.parse(toastCfg)); } catch (_) {}
    }
  });

  // ─── notification bell ───────────────────────────────────────────

  // The spoken count follows the signal the badge follows: the kernel
  // writes the number into the bound nodes, and the module re-formats
  // the anchor's accessible name through the sentence shape the
  // component rendered.
  document.addEventListener('gofastr:signal', function (e) {
    const d = e && e.detail;
    if (!d || typeof d.name !== 'string') return;
    for (const bell of document.querySelectorAll('[data-hui-notification-bell]')) {
      if (bell.getAttribute('data-cui-signal') !== d.name) continue;
      const fmt = bell.getAttribute('data-hui-notification-count-fmt') || '';
      const n = parseInt(d.value, 10);
      if (!Number.isFinite(n)) return;
      if (fmt) {
        bell.setAttribute('aria-label', fmt.replace('%d', String(n)).replace('%d', String(n)));
      }
      // The badge's count attribute follows the signal too, so the
      // next reader of it (a stylesheet's 99+ shaping, a test) sees
      // the same number the anchor says.
      const badge = bell.querySelector('[data-hui-notification-count]');
      if (badge) badge.setAttribute('data-hui-notification-count', String(n));
    }
  });

  // ─── network retry ───────────────────────────────────────────────

  // The banner itself is the headless module's offline SystemBanner;
  // this half owns the retry: the anchor is a real link (no script =
  // the page reloads through it), and with script the fetch happens
  // in place — a 2xx reports recovery and hides the banner, a failed
  // probe leaves it shown.
  document.addEventListener('click', function (e) {
    const t = e.target;
    const retry = t && t.closest && t.closest('[data-hui-network-retry]');
    if (!retry) return;
    e.preventDefault();
    const href = retry.getAttribute('href') || '';
    if (!href) return;
    // A 2xx from the health endpoint is the reconnect: hide the
    // banner (reportRecovery drives every mounted offline one). A
    // failed probe leaves it shown — the connection is still down
    // and hiding it would lie.
    fetch(href, { credentials: 'same-origin' })
      .then(function (r) { if (r.ok) NS.networkStatus.reportRecovery(); })
      .catch(function () {});
  });

  // The public status API app code called on the retired module,
  // kept on the namespace so callers do not break: reportFailure and
  // reportRecovery drive every mounted offline banner's data-state.
  NS.networkStatus = NS.networkStatus || {};
  NS.networkStatus.reportFailure = function () {
    for (const b of document.querySelectorAll('[data-hui-system-offline]')) b.hidden = false;
  };
  NS.networkStatus.reportRecovery = function () {
    for (const b of document.querySelectorAll('[data-hui-system-offline]')) b.hidden = true;
  };

  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const c of within(scope, '[data-hui-toast-stack],[data-cui-toast-stack]')) NS._initToasts(c);
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
