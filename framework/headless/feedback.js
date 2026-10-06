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
(() => {
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

  NS._initToasts = (root) => {
    // A row the module built carries its id; a row the server
    // rendered carries the toast marker and, when it has a lifetime,
    // the TTL. Both arm here; a server row is given its id on sight.
    const items = root.querySelectorAll('[data-hui-toast-id], [data-hui-toast-ttl-ms]');
    const present = new Set();
    items.forEach((item) => {
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
        const arm = () => {
          rec.startedAt = Date.now();
          rec.timer = setTimeout(() => { NS._dismissToast(item, id); }, rec.remaining);
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
      // A server row's dismiss is a link with its Island: the kernel
      // owns that click. The module's own rows dismiss here.
      if (item.hasAttribute('data-hui-toast')) return;
      item.addEventListener('click', (e) => {
        if (e.target.closest('[data-hui-toast-dismiss]')) {
          e.preventDefault();
          NS._dismissToast(item, id);
        }
      });
    });
    NS._toastTimers.forEach((rec, id) => {
      if (!present.has(id)) {
        clearTimeout(rec.timer);
        NS._toastTimers.delete(id);
      }
    });
  };

  NS._dismissToast = (item, id) => {
    if (!item || item.hasAttribute('data-hui-toast-leaving')) return;
    // The leaving mark is the module's own (written here, rendered by
    // no component): the kit's stack sheet animates the row out on it.
    item.setAttribute('data-hui-toast-leaving', '');
    const rec = NS._toastTimers.get(id);
    if (rec) { clearTimeout(rec.timer); NS._toastTimers.delete(id); }
    const ms = parseFloat(getComputedStyle(item).animationDuration) * 1000 || 200;
    setTimeout(() => { if (item.parentNode) item.parentNode.removeChild(item); }, ms);
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
    ['data-hui-toast-tone', 'data-hui-toast-icon', 'data-hui-toast-title', 'data-hui-toast-body'].forEach((hook) => {
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

  // templateVariant: the class and glyph the template lists for a
  // variant beyond the four tones (data-hui-toast-variants, a JSON map
  // the kit renders). A name the template does not list, or a map that
  // does not parse, is a row with no variant class and no glyph.
  function templateVariant(tpl, variant) {
    const none = { cls: '', glyph: '' };
    let map = null;
    try { map = JSON.parse(tpl.getAttribute('data-hui-toast-variants') || 'null'); } catch (_) { return none; }
    if (!map || typeof map !== 'object' || !Object.prototype.hasOwnProperty.call(map, variant)) return none;
    const v = map[variant];
    if (!v || typeof v !== 'object') return none;
    return {
      cls: typeof v.class === 'string' ? v.class : '',
      glyph: typeof v.glyph === 'string' ? v.glyph : ''
    };
  }

  NS.toast = (cfg) => {
    if (cfg == null) return null;
    if (typeof cfg === 'string') cfg = { title: cfg, ttl: 4000 };
    if (!cfg.title) return null;
    const container = findStack(cfg.stack);
    if (!container) return null;

    const id = 't' + (++NS._toastSeq);
    // The kernel's own failures say 'error' (formerrors.js, rpc.js):
    // that is the danger tone by another name.
    let variant = String(cfg.variant || 'info');
    if (variant === 'error') variant = 'danger';
    // A tone gets its word, its glyph and its class from the per-tone
    // attributes. Any other variant (neutral, a registered status
    // variant) is not a tone: it wears what the template lists for it
    // and says no tone word, so it never passes for info.
    const tone = TONES.indexOf(variant) >= 0 ? variant : '';
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
    let variantCls = '';
    let glyph = '';
    if (tpl && tone) {
      variantCls = tpl.getAttribute(VARIANT_ATTR[tone]) || '';
      glyph = tpl.getAttribute(GLYPH_ATTR[tone]) || '';
    } else if (tpl) {
      const extra = templateVariant(tpl, variant);
      variantCls = extra.cls;
      glyph = extra.glyph;
    }
    variantCls.split(/\s+/).forEach((c) => { if (c) root.classList.add(c); });
    root.setAttribute('role', assertive ? 'alert' : 'status');
    root.setAttribute('aria-live', assertive ? 'assertive' : 'polite');

    const toneWord = tone ? readWord(container, tpl, TONE_ATTR[tone]) : '';
    fill(root, 'data-hui-toast-tone', toneWord ? toneWord + ': ' : '');
    fill(root, 'data-hui-toast-icon', glyph);
    fill(root, 'data-hui-toast-title', cfg.title);
    fill(root, 'data-hui-toast-body', cfg.body || '');
    const dismiss = root.querySelector('[data-hui-toast-dismiss]');
    if (dismiss) {
      const fmt = readWord(container, tpl, 'data-hui-toast-dismiss-label') || '%s';
      dismiss.setAttribute('aria-label', fmt.replace('%s', () => cfg.title));
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
  document.addEventListener('click', (e) => {
    const t = e.target;
    const wrap = t && t.closest && t.closest('[data-hui-copy]');
    if (!wrap) return;
    const id = wrap.getAttribute('data-hui-copy-target');
    const target = id ? document.getElementById(id) : null;
    if (!target) return;
    // A <pre> holds its source text verbatim, newlines included, so its
    // textContent is the copy. innerText re-derives line breaks from the
    // layout: a block per line doubles every break, and an empty block
    // (a blank source line) adds none.
    const text = (target.tagName === 'PRE' ? target.textContent
      : (target.innerText || target.textContent) || '').trim();
    if (!text) return;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).catch(() => {});
    }
    wrap.setAttribute('data-hui-copy-state', 'done');
    const btn = wrap.querySelector('[data-hui-copy-label]');
    const fmts = wrap.getAttribute('data-hui-copy-copied') || '';
    if (btn && fmts) btn.textContent = fmts;
    const status = wrap.querySelector('[data-hui-copy-status]');
    if (status) {
      const name = wrap.getAttribute('data-hui-copy-name') || id || '';
      const sentence = (wrap.getAttribute('data-hui-copy-sentence') || '').replace('{name}', () => name);
      status.textContent = '';
      requestAnimationFrame(() => { status.textContent = sentence; });
    }
    const back = wrap.getAttribute('data-hui-copy-back') || '';
    setTimeout(() => {
      wrap.removeAttribute('data-hui-copy-state');
      if (btn && back) btn.textContent = back;
    }, 1200);
    // A toast on copy rides this module's own toast runtime; the
    // config JSON is read from the wrapper itself (a menu's copy row)
    // or whichever element under it carries it (ui.CopyButton puts it
    // on the button).
    const toastCfg = (wrap.querySelector('[data-hui-copy-toast]') || wrap).getAttribute('data-hui-copy-toast');
    if (toastCfg) {
      try { NS.toast(JSON.parse(toastCfg)); } catch (_) {}
    }
  });

  // ─── notification bell ───────────────────────────────────────────

  // The spoken count follows the badge: the kernel writes a bound
  // signal's value into the badge span (UnreadBind puts the binding on
  // the badge, not the anchor), and an observer on that span's text
  // re-formats the anchor's accessible name through the sentence shape
  // the component rendered. Watching the badge, not the signal store,
  // means no subscription outlives a bell a navigation removed.
  const bellsWatched = new WeakSet();
  function sayBellCount(bell, badge) {
    const text = (badge.textContent || '').trim();
    // An empty badge is the signal's "nothing unread" (the sheet hides
    // it); anything else that is not a whole count leaves the name
    // alone: parseInt would read "12x" as 12 and announce a number the
    // badge does not show.
    if (!/^\d{0,9}$/.test(text)) return;
    const n = +text;
    const fmt = bell.getAttribute('data-hui-notification-count-fmt') || '';
    if (fmt) bell.setAttribute('aria-label', fmt.split('%d').join(n));
    // The badge's count attribute follows too, so the next reader of
    // it (a stylesheet's 99+ shaping, a test) sees the same number the
    // anchor says. An attribute write, so the observer does not hear it.
    badge.setAttribute('data-hui-notification-count', n);
  }
  function watchBell(bell) {
    if (bellsWatched.has(bell) || typeof MutationObserver !== 'function') return;
    const badge = bell.querySelector('[data-hui-notification-count]');
    if (!badge) return;
    bellsWatched.add(bell);
    new MutationObserver(() => { sayBellCount(bell, badge); })
      .observe(badge, { childList: true, characterData: true, subtree: true });
  }

  // ─── network retry ───────────────────────────────────────────────

  // The banner itself is the headless module's offline SystemBanner;
  // this half owns the retry: the anchor is a real link (no script =
  // the page reloads through it), and with script the fetch happens
  // in place — a 2xx reports recovery and hides the banner, a failed
  // probe leaves it shown.
  document.addEventListener('click', (e) => {
    const t = e.target;
    const retry = t && t.closest && t.closest('[data-hui-network-retry]');
    if (!retry) return;
    e.preventDefault();
    const href = retry.getAttribute('href') || '';
    // One probe at a time: a click while one is in flight is a no-op.
    if (!href || retry.getAttribute('aria-busy')) return;
    // While the probe runs, the link is aria-busy and the banner root
    // says data-state="checking" (the busy state a styled banner
    // dresses); both clear when the probe settles, either way. Nothing
    // else in the module writes the banner's data-state.
    const banner = retry.closest('[data-hui-system]') || retry;
    retry.setAttribute('aria-busy', 'true');
    banner.setAttribute('data-state', 'checking');
    // A 2xx from the health endpoint is the reconnect: hide the
    // banner (reportRecovery drives every mounted offline one). A
    // failed probe leaves it shown — the connection is still down
    // and hiding it would lie.
    fetch(href, { credentials: 'same-origin' })
      .then((r) => { if (r.ok) NS.networkStatus.reportRecovery(); }, () => {})
      .finally(() => { retry.removeAttribute('aria-busy'); banner.removeAttribute('data-state'); });
  });

  // The public status API app code called on the retired module,
  // kept on the namespace so callers do not break: reportFailure and
  // reportRecovery drive every mounted offline banner's data-state.
  NS.networkStatus = NS.networkStatus || {};
  NS.networkStatus.reportFailure = () => {
    for (const b of document.querySelectorAll('[data-hui-system-offline]')) b.hidden = false;
  };
  NS.networkStatus.reportRecovery = () => {
    for (const b of document.querySelectorAll('[data-hui-system-offline]')) b.hidden = true;
  };

  // ─── the arrival pass ────────────────────────────────────────────

  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    for (const c of within(scope, '[data-hui-toast-stack],[data-cui-toast-stack]')) NS._initToasts(c);
    for (const bell of within(scope, '[data-hui-notification-bell]')) watchBell(bell);
  }

  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
