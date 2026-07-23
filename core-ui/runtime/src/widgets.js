// GoFastr runtime module — Widgets
//
// The widget runtime: mountWidget (chrome + dismiss + modal stack),
// openWidget / closeWidget / _mountByName / chrome cache. The
// per-widget data-fui-* primitives now live in demand-loaded sibling
// modules: widgethelpers (charcount, persist-storage, fill-input,
// clear-on-esc, submit-on-enter, disable-when-invalid, tick-elapsed),
// widgetfocus (Escape + Tab focus trap), widgetlinks (deep-link
// push/strip), textarea (autogrow), shortcut (chords). mountWidget
// demand-loads them; re-wiring on remount/poll-swap happens through
// the core rescan loops (each module registers a _moduleScanners entry
// and sets loadedModules).
//
// Loads on demand:
//   - core's marker scanner picks up [data-fui-widget] (SSR-inlined
//     auto-mount widgets) and [data-fui-open] (click triggers) and
//     idle-loads this module.
//   - core's data-fui-open click delegator awaits loadModule('widgets')
//     before calling openWidget.
//   - core's auto-mount loop (the catalog fetch) awaits loadModule('widgets')
//     before iterating mounts.
//
// State stays on window.__gofastr — _widgets, _modalStack,
// _popoverStack, _signals, _focusSel — so the popover module and
// core can still read them.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  NS._chromeCache = NS._chromeCache || {};

  NS.openWidget = async function (name, opts) {
    const entry = NS._widgetCatalog && NS._widgetCatalog[name];
    if (!entry) {
      // Static export + the target widget isn't registered (e.g. a note-only
      // showcase demo whose modal was never mounted). Don't fail silently —
      // tell the visitor why nothing opened. _fallbackToast is synchronous
      // and already on NS, so no module fetch / async wait.
      if (document.documentElement.hasAttribute('data-fui-static') && NS._fallbackToast) {
        NS._fallbackToast({ title: 'Needs the Go server.' });
      }
      return;
    }
    const o = opts || {};
    const params = o.params || {};
    const cfg = entry.cfg;
    await NS._mountByName(name);
    const declared = cfg.deepLinkParams || [];
    if (declared.length) {
      const url = new URL(window.location.href);
      for (const k of declared) {
        const v = (k in params) ? params[k] : url.searchParams.get(k);
        if (v != null) NS.setSignal(k, v);
      }
    }
    if (o.pushUrl && cfg.deepLinkKey && cfg.deepLinkValue) {
      if (!NS._deepLinkPushUrl) await NS.loadModule('widgetlinks');
      // Skip if dismissed while the module loaded (cold cache).
      if (!NS._widgets[name]) return;
      NS._deepLinkPushUrl(cfg, params);
    }
  };

  NS._mountByName = async function (name) {
    const entry = NS._widgetCatalog && NS._widgetCatalog[name];
    if (!entry) return;
    if (NS._widgets[name]) return; // already mounted
    const cfg = entry.cfg;
    const existing = document.querySelector('[data-fui-widget="' + CSS.escape(name) + '"]');
    if (existing) {
      NS.mountWidget(cfg, null, existing);
      return;
    }
    const path = cfg.chromePath || ('/core-ui/widget/' + name + '/chrome');
    if (!NS._chromeCache[name]) {
      NS._chromeCache[name] = (async () => {
        try {
          const r = await fetch(path);
          if (!r.ok) throw new Error('chrome fetch ' + r.status);
          return await r.text();
        } catch (err) {
          delete NS._chromeCache[name];
          throw err;
        }
      })();
    }
    let html = '';
    try { html = await NS._chromeCache[name]; } catch (_) {}
    if (html) NS.mountWidget(cfg, html);
  };

  NS.closeWidget = function (name) {
    const w = NS._widgets[name];
    if (w && typeof w.dismiss === 'function') w.dismiss();
  };

  NS._syncDeepLinks = function () {
    const idx = NS._widgetDeepLinks || {};
    const url = new URL(window.location.href);
    for (const key in idx) {
      const got = url.searchParams.get(key);
      for (const entry of idx[key]) {
        const mounted = !!NS._widgets[entry.name];
        if (got === entry.value && !mounted) {
          NS.openWidget(entry.name, { pushUrl: false });
        } else if (got !== entry.value && mounted) {
          NS.closeWidget(entry.name);
        }
      }
    }
  };

  NS.mountWidget = function (cfg, html, existing) {
    if (NS._widgets[cfg.name]) return; // already mounted
    NS._widgets[cfg.name] = { cfg };

    // Stylesheet
    if (!document.querySelector('link[data-fui-style="' + cfg.name + '"]')) {
      const link = document.createElement('link');
      link.rel = 'stylesheet';
      link.href = cfg.stylePath;
      link.setAttribute('data-fui-style', cfg.name);
      document.head.appendChild(link);
    }

    // Backdrop + chrome.
    let backdrop = null;
    if (cfg.backdrop) {
      backdrop = document.querySelector('[data-fui-backdrop="' + CSS.escape(cfg.name) + '"]');
      if (!backdrop) {
        backdrop = document.createElement('div');
        backdrop.className = 'fui-backdrop overlay-backdrop';
        backdrop.setAttribute('data-fui-backdrop', cfg.name);
        NS.doc.appendBody(backdrop);
      }
    }
    let w;
    if (existing) {
      w = existing;
      w.removeAttribute('hidden');
    } else if (html) {
      const tmp = document.createElement('div');
      tmp.innerHTML = html;
      w = tmp.firstElementChild;
      NS.doc.appendBody(w);
    } else {
      delete NS._widgets[cfg.name];
      return;
    }
    const reg = NS._widgets[cfg.name];
    reg.root = w;
    reg.backdrop = backdrop;
    reg.hydrated = !!existing;
    NS.scanAndLoadCSS(w);
    if (w.querySelector('[data-fui-fill-input],[data-fui-tick-elapsed],[data-fui-persist-storage],[data-fui-charcount-source],[data-fui-clear-on-esc],form[data-fui-submit-on-enter],form[data-fui-disable-when-invalid]')) NS.loadModule('widgethelpers');
    if (cfg.closeOnEscape || cfg.backdrop) NS.loadModule('widgetfocus');
    if (cfg.deepLinkKey) NS.loadModule('widgetlinks');

    const isModal = !!cfg.backdrop;
    const pf = isModal ? document.activeElement : null;
    if (isModal) {
      // Owner-refcounted viewport lock (NS.doc) — the lock releases only
      // when the LAST owner unlocks, so a second locker (lightbox,
      // drawer) can't release a modal's lock early. NS.doc locks <html>,
      // not <body>: overflow:hidden on <body> turns the body into a
      // clipped scroll container, which breaks any position:sticky
      // descendant (a docs nav rail scrolls off-screen on a scrolled
      // page). The root element locks the viewport just as well while
      // leaving sticky elements pinned. Scroll position is preserved.
      NS.doc.lockScroll('widget:' + cfg.name);
      NS._modalStack.push(cfg.name);
      Promise.resolve().then(() => {
        // Prefer an explicit [autofocus] element if the slot author
        // marked one — that's how ui.ConfirmAction opts into focusing
        // the Confirm button instead of Cancel. We also strip the
        // attribute after focusing so the platform's native autofocus
        // pass (which runs when the element next enters the DOM, e.g.
        // after re-open) doesn't race with this focus() call. Chrome
        // logs "Autofocus processing was blocked because a document
        // already has a focused element" when both fire.
        // preventScroll: a modal/drawer is fixed/centred and already in
        // view, so focusing its first control must NOT scroll the document.
        // Without this, focus races the demand-loaded position:fixed CSS and
        // scrolls the page to the element's transient in-flow position
        // (a tall drawer jumps the page by its own height on open).
        const explicit = w.querySelector('[autofocus]');
        if (explicit) {
          explicit.removeAttribute('autofocus');
          explicit.focus({ preventScroll: true });
          return;
        }
        const focusables = w.querySelectorAll(NS._focusSel);
        if (focusables.length > 0) focusables[0].focus({ preventScroll: true });
      });
    }

    function dismiss() {
      const st = NS._widgets[cfg.name] || {};
      const hydrated = st.hydrated;
      const oh = st.outsideHandler;
      const ar = st.anchorResize;
      const as = st.anchorScroll;
      const at = st.anchorTrigger;
      const stop = st.pollStop;
      if (oh) document.removeEventListener('click', oh);
      if (ar) window.removeEventListener('resize', ar);
      if (as) window.removeEventListener('scroll', as, { capture: true });
      if (at) {
        at.classList.remove('is-popover-trigger-active');
        at.removeAttribute('data-fui-popover-trigger');
      }
      if (stop) stop();
      if (w && w.style) {
        w.style.left = '';
        w.style.top = '';
        w.style.right = '';
        w.style.bottom = '';
        w.style.position = '';
        w.style.removeProperty('--ui-popover-arrow-x');
        w.style.removeProperty('--ui-popover-arrow-y');
        w.removeAttribute('data-fui-popover-side');
      }
      if (hydrated && w) {
        w.setAttribute('hidden', '');
      } else if (w?.parentNode) {
        w.parentNode.removeChild(w);
      }
      if (backdrop?.parentNode) backdrop.parentNode.removeChild(backdrop);
      delete NS._widgets[cfg.name];
      if (NS._popoverStack && NS._popoverStack.length) {
        const pIdx = NS._popoverStack.indexOf(cfg.name);
        if (pIdx >= 0) NS._popoverStack.splice(pIdx, 1);
      }
      if (isModal) {
        const idx = NS._modalStack.indexOf(cfg.name);
        if (idx >= 0) NS._modalStack.splice(idx, 1);
        NS.doc.unlockScroll('widget:' + cfg.name);
        // preventScroll: restoring focus to the trigger on close must not
        // scroll the page to it (the trigger may be off-screen after the
        // user scrolled), which otherwise jumps the page on dismiss.
        if (pf && typeof pf.focus === 'function') {
          try { pf.focus({ preventScroll: true }); } catch (_) {}
        }
      }
      // mountWidget starts the widgetlinks load for every deep-linkable
      // widget, so the strip helper is only absent inside the module's
      // initial in-flight window (milliseconds after an auto-open) —
      // an accepted race; the URL then simply keeps the deep link the
      // user navigated to.
      if (cfg.deepLinkKey && cfg.deepLinkValue && NS._deepLinkStripUrl) NS._deepLinkStripUrl(cfg);
    }
    reg.dismiss = dismiss;

    // Initial state hydration — only when the widget declared signals.
    if (cfg.statePath) {
      fetch(cfg.statePath, { headers: { 'X-FUI-Widget': cfg.name } })
        .then((r) => (r.ok ? r.json() : {}))
        .then((state) => {
          for (const k in state) {
            const existing = NS._signals[k];
            if (existing && existing.value !== undefined) continue;
            NS.setSignal(k, state[k]);
          }
        })
        .catch(() => {});
    }

    // Polling freshness — when the widget declares BOTH a statePath
    // (it has signals) AND a pollMs (Builder.Poll), the runtime
    // re-fetches statePath on the cadence and overwrites each
    // declared signal with the fresh value. Mount hydration above
    // skips already-set signals; polling intentionally does NOT —
    // the whole point is to replace stale values. We still skip the
    // DOM write when setSignal would be a no-op (value unchanged)
    // so bound nodes don't flash/re-layout on every tick.
    //
    // Semantics shared with data-fui-poll (src/poll.js):
    //   - ±10% jitter per interval (desynchronise multi-widget polls)
    //   - pause while document.hidden; on visibilitychange → fetch
    //     immediately and resume
    //   - on fetch failure: double the interval, cap at 5× base,
    //     reset to base on the next success
    if (cfg.pollMs && cfg.statePath) {
      // The poll implementation lives in the demand-loaded poll module
      // (shared cadence/back-off/pollStatus machinery with data-fui-poll)
      // so widget-bearing pages that never poll don't ship it. _widgetPoll
      // installs pollStop/pollNow on the widget entry.
      NS.loadModule('poll').then(() => {
        if (NS._widgetPoll) NS._widgetPoll(cfg, NS._widgets[cfg.name]);
      }).catch(() => {});
    }

    async function dispatchRPC(node) {
      const path = node.getAttribute('data-fui-rpc');
      const method = (node.getAttribute('data-fui-rpc-method') || 'POST').toUpperCase();
      const sig = node.getAttribute('data-fui-rpc-signal');
      const close = node.hasAttribute('data-fui-rpc-close');
      const reset = node.hasAttribute('data-fui-rpc-reset') && node.tagName === 'FORM';
      let body = node.getAttribute('data-fui-rpc-body');
      let formData = false;
      if (!body && node.tagName === 'FORM') {
        const fd = new FormData(node);
        if (node.enctype === 'multipart/form-data' || node.querySelector('input[type="file"]')) {
          body = fd;
          formData = true;
        } else {
          const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
          body = JSON.stringify(obj);
        }
      }
      const headers = { 'X-FUI-Widget': cfg.name };
      if (body && !formData) headers['Content-Type'] = 'application/json';
      // CSRF: forward the page's <meta name="csrf-token"> via the
      // X-CSRF-Token header. JSON/multipart RPC bodies can't carry the
      // urlencoded `_csrf` field the auth.CSRF middleware parses, so the
      // header is the only working channel. Mirrors the core dispatchRPC.
      const csrfMeta = document.querySelector('meta[name="csrf-token"]');
      if (csrfMeta) {
        const tok = csrfMeta.getAttribute('content');
        if (tok) headers['X-CSRF-Token'] = tok;
      }
      if (node.tagName === 'BUTTON' || node.tagName === 'INPUT') node.disabled = true;
      // Task C: add fui-loading CSS class and aria-busy for styling during in-flight RPC.
      node.classList.add('fui-loading');
      node.setAttribute('aria-busy', 'true');
      try {
        const r = await fetch(path, { method, headers, body: body || undefined, credentials: 'same-origin' });
        if (!r.ok) {
          const txt = await r.text();
          if (sig) NS.setSignal(sig, { ok: false, status: r.status, text: txt });
          return;
        }
        const toast = r.headers.get('X-Gofastr-Toast');
        if (toast) {
          NS.loadModule('toasts').then(() => {
            try {
              const parsed = JSON.parse(toast);
              const arr = Array.isArray(parsed) ? parsed : [parsed];
              for (const cfg of arr) NS.toast(cfg);
            } catch (_) {}
          }).catch(() => {});
        }
        const ct = r.headers.get('content-type') || '';
        const data = ct.indexOf('application/json') >= 0 ? await r.json() : await r.text();
        if (sig) NS.setSignal(sig, data);
        // Mutation → authoritative refresh: a successful RPC likely
        // changed server state a polling widget renders, so re-fetch
        // /state now instead of waiting out the cadence. Default target
        // is the widget the button lives in; data-fui-rpc-refresh names
        // a DIFFERENT widget — e.g. a Reset button inside a confirm
        // modal (kiln-reset-confirm) refreshing the chat panel
        // (kiln-panel), which its own closure would never reach.
        const refresh = node.getAttribute('data-fui-rpc-refresh') || cfg.name;
        const wentry = NS._widgets[refresh];
        if (wentry && wentry.pollNow) wentry.pollNow();
        if (close) dismiss();
        if (reset) node.reset();
        // Open a widget on success (e.g. "save in drawer → open results sheet").
        const open = node.getAttribute('data-fui-rpc-open');
        if (open) NS.openWidget(open);
        // SPA navigate on success. force: the RPC mutated server state,
        // so bypass the screen cache and re-render even when the
        // destination is the page the widget floats over (quick-add
        // modal on the list it inserts into).
        const nav = node.getAttribute('data-fui-rpc-navigate');
        if (nav) {
          NS.navigate(nav, { force: true });
        }
      } catch (err) {
        // Network error: write human-readable feedback to the signal.
        if (sig) {
          NS.setSignal(sig, { ok: false, status: 0, text: 'Network error \u2014 please try again' });
        }
      } finally {
        if (node.tagName === 'BUTTON' || node.tagName === 'INPUT') node.disabled = false;
        // Task C: remove fui-loading CSS class and aria-busy after RPC completes.
        node.classList.remove('fui-loading');
        node.removeAttribute('aria-busy');
      }
    }

    // Widget-scoped click + submit.
    w.addEventListener('click', async (e) => {
      const btn = e.target.closest('[data-fui-rpc]');
      if (btn && w.contains(btn) && btn.tagName !== 'FORM') {
        e.preventDefault();
        await dispatchRPC(btn);
        return;
      }
      const closeBtn = e.target.closest('[data-fui-action="close"]');
      if (closeBtn && w.contains(closeBtn)) {
        e.preventDefault();
        dismiss();
      }
    });
    w.addEventListener('submit', async (e) => {
      const form = e.target.closest('form[data-fui-rpc]');
      if (form && w.contains(form)) {
        e.preventDefault();
        await dispatchRPC(form);
      }
    });

    if (cfg.closeOnClick && backdrop) backdrop.addEventListener('click', dismiss);
    if (isModal) reg.closeOnEscape = !!cfg.closeOnEscape;

    if (!isModal && (cfg.closeOnEscape || cfg.closeOnClick)) {
      reg.closeOnEscape = !!cfg.closeOnEscape;
      reg.closeOnClickOutside = !!cfg.closeOnClick;
      (NS._popoverStack ||= []).push(cfg.name);
      if (cfg.closeOnClick) {
        const oh = (e) => {
          if (w.contains(e.target)) return;
          const trigger = e.target.closest('[data-fui-open="' + cfg.name + '"]');
          if (trigger) return;
          dismiss();
        };
        NS._widgets[cfg.name].outsideHandler = oh;
        setTimeout(() => document.addEventListener('click', oh), 0);
      }
    }

  };

  (NS.loadedModules ||= {}).widgets = true;
})();
