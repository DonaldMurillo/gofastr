// GoFastr runtime module — Widgets
//
// The widget runtime: mountWidget (chrome + dismiss + modal stack +
// focus trap), openWidget / closeWidget / _mountByName / chrome cache,
// deep-link push/strip/sync, and the modal-stack Escape + Tab focus
// trap handlers. All the per-widget data-fui-* primitive scanners
// (autogrow, charcount, persist-storage, fill-input, clear-on-esc,
// submit-on-enter, disable-when-invalid, copy-text-from, shortcuts,
// tick-elapsed) move with mountWidget too — they were scoped inside
// the mountWidget closure and stay that way.
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
    if (!entry) return;
    const o = opts || {};
    const params = o.params || {};
    await NS._mountByName(name);
    const declared = entry.cfg.deepLinkParams || [];
    if (declared.length) {
      const url = new URL(window.location.href);
      for (const k of declared) {
        const v = (k in params) ? params[k] : url.searchParams.get(k);
        if (v != null) NS.setSignal(k, v);
      }
    }
    if (o.pushUrl && entry.cfg.deepLinkKey && entry.cfg.deepLinkValue) {
      NS._deepLinkPushUrl(entry.cfg, params);
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

  NS._deepLinkPushUrl = function (cfg, params) {
    const url = new URL(window.location.href);
    url.searchParams.set(cfg.deepLinkKey, cfg.deepLinkValue);
    for (const k of cfg.deepLinkParams || []) {
      if (k in params) url.searchParams.set(k, params[k]);
    }
    if (url.href !== window.location.href) {
      history.pushState(null, '', url.pathname + url.search + url.hash);
    }
  };

  NS._deepLinkStripUrl = function (cfg) {
    const url = new URL(window.location.href);
    let touched = false;
    if (url.searchParams.get(cfg.deepLinkKey) === cfg.deepLinkValue) {
      url.searchParams.delete(cfg.deepLinkKey);
      touched = true;
    }
    for (const k of cfg.deepLinkParams || []) {
      if (url.searchParams.has(k)) { url.searchParams.delete(k); touched = true; }
    }
    if (touched) {
      const s = url.searchParams.toString();
      history.pushState(null, '', url.pathname + (s ? '?' + s : '') + url.hash);
    }
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

  NS.mountWidget = function (cfg, chromeHTML, existingEl) {
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
        document.body.appendChild(backdrop);
      }
    }
    let widgetEl;
    if (existingEl) {
      widgetEl = existingEl;
      widgetEl.removeAttribute('hidden');
    } else if (chromeHTML) {
      const tmp = document.createElement('div');
      tmp.innerHTML = chromeHTML;
      widgetEl = tmp.firstElementChild;
      document.body.appendChild(widgetEl);
    } else {
      delete NS._widgets[cfg.name];
      return;
    }
    NS._widgets[cfg.name].root = widgetEl;
    NS._widgets[cfg.name].backdrop = backdrop;
    NS._widgets[cfg.name].hydrated = !!existingEl;
    NS.scanAndLoadCSS(widgetEl);

    const isModal = !!cfg.backdrop;
    const previousFocus = isModal ? document.activeElement : null;
    if (isModal) {
      if (NS._modalStack.length === 0) document.body.style.overflow = 'hidden';
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
        const explicit = widgetEl.querySelector('[autofocus]');
        if (explicit) {
          explicit.removeAttribute('autofocus');
          explicit.focus({ preventScroll: true });
          return;
        }
        const focusables = widgetEl.querySelectorAll(NS._focusSel);
        if (focusables.length > 0) focusables[0].focus({ preventScroll: true });
      });
    }

    function dismiss() {
      const wasHydrated = NS._widgets[cfg.name]?.hydrated;
      const outsideHandler = NS._widgets[cfg.name]?.outsideHandler;
      const anchorResize = NS._widgets[cfg.name]?.anchorResize;
      const anchorScroll = NS._widgets[cfg.name]?.anchorScroll;
      const anchorTrigger = NS._widgets[cfg.name]?.anchorTrigger;
      if (outsideHandler) document.removeEventListener('click', outsideHandler);
      if (anchorResize) window.removeEventListener('resize', anchorResize);
      if (anchorScroll) window.removeEventListener('scroll', anchorScroll, { capture: true });
      if (anchorTrigger) {
        anchorTrigger.classList.remove('is-popover-trigger-active');
        anchorTrigger.removeAttribute('data-fui-popover-trigger');
      }
      if (widgetEl && widgetEl.style) {
        widgetEl.style.left = '';
        widgetEl.style.top = '';
        widgetEl.style.right = '';
        widgetEl.style.bottom = '';
        widgetEl.style.position = '';
        widgetEl.style.removeProperty('--ui-popover-arrow-x');
        widgetEl.style.removeProperty('--ui-popover-arrow-y');
        widgetEl.removeAttribute('data-fui-popover-side');
      }
      if (wasHydrated && widgetEl) {
        widgetEl.setAttribute('hidden', '');
      } else if (widgetEl?.parentNode) {
        widgetEl.parentNode.removeChild(widgetEl);
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
        if (NS._modalStack.length === 0) document.body.style.overflow = '';
        // preventScroll: restoring focus to the trigger on close must not
        // scroll the page to it (the trigger may be off-screen after the
        // user scrolled), which otherwise jumps the page on dismiss.
        if (previousFocus && typeof previousFocus.focus === 'function') {
          try { previousFocus.focus({ preventScroll: true }); } catch (_) {}
        }
      }
      if (cfg.deepLinkKey && cfg.deepLinkValue) NS._deepLinkStripUrl(cfg);
      // Close any widget-scoped SSE streams opened during mount so
      // the server-side connection is freed on every modal close.
      const streams = NS._widgets[cfg.name]?.seenStreams;
      if (streams) {
        for (const path in streams) {
          const es = streams[path];
          if (es && typeof es.close === 'function') es.close();
        }
      }
    }
    NS._widgets[cfg.name].dismiss = dismiss;

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

    // SSE bindings (per-widget, separate from the document-level
    // sse.js island stream). seenStreams is stored on the widget entry
    // so dismiss() can close them (see close loop above).
    const seenStreams = {};
    NS._widgets[cfg.name].seenStreams = seenStreams;
    for (const b of cfg.sse || []) {
      if (!seenStreams[b.path]) {
        try {
          const es = new EventSource(b.path);
          seenStreams[b.path] = es;
          es.addEventListener('open', () => {
            window.__fuiSSEReady = true;
            document.body.classList.remove('fui-sse-down');
            document.body.classList.add('fui-sse-up');
          });
          es.addEventListener('error', () => {
            document.body.classList.remove('fui-sse-up');
            document.body.classList.add('fui-sse-down');
          });
        } catch (_) {
          seenStreams[b.path] = null;
        }
      }
      const es = seenStreams[b.path];
      if (!es) continue;
      if (b.reload) {
        es.addEventListener(b.event, (ev) => {
          if (b.match) {
            let payload = {};
            try { payload = JSON.parse(ev.data) || {}; } catch (_) {}
            for (const k in b.match) {
              if (String(payload[k]) !== String(b.match[k])) return;
            }
          }
          setTimeout(() => location.reload(), 200);
        });
        continue;
      }
      if (b.refetch) {
        es.addEventListener(b.event, () => {
          fetch(cfg.statePath, { headers: { 'X-FUI-Widget': cfg.name } })
            .then((r) => (r.ok ? r.json() : null))
            .then((state) => {
              if (state && b.signal in state) NS.setSignal(b.signal, state[b.signal]);
            })
            .catch(() => {});
        });
      } else {
        es.addEventListener(b.event, (ev) => {
          let payload;
          try { payload = JSON.parse(ev.data); } catch (_) { payload = ev.data; }
          NS.setSignal(b.signal, payload);
        });
      }
    }

    async function dispatchRPC(node) {
      const path = node.getAttribute('data-fui-rpc');
      const method = (node.getAttribute('data-fui-rpc-method') || 'POST').toUpperCase();
      const responseSignal = node.getAttribute('data-fui-rpc-signal');
      const closeOnSuccess = node.hasAttribute('data-fui-rpc-close');
      const resetOnSuccess = node.hasAttribute('data-fui-rpc-reset') && node.tagName === 'FORM';
      let body = node.getAttribute('data-fui-rpc-body');
      let bodyIsFormData = false;
      if (!body && node.tagName === 'FORM') {
        const fd = new FormData(node);
        if (node.enctype === 'multipart/form-data' || node.querySelector('input[type="file"]')) {
          body = fd;
          bodyIsFormData = true;
        } else {
          const obj = {}; fd.forEach((v, k) => { obj[k] = v; });
          body = JSON.stringify(obj);
        }
      }
      const headers = { 'X-FUI-Widget': cfg.name };
      if (body && !bodyIsFormData) headers['Content-Type'] = 'application/json';
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
          if (responseSignal) NS.setSignal(responseSignal, { ok: false, status: r.status, text: txt });
          return;
        }
        const toastHeader = r.headers.get('X-Gofastr-Toast');
        if (toastHeader) {
          NS.loadModule('toasts').then(() => {
            try {
              const parsed = JSON.parse(toastHeader);
              const arr = Array.isArray(parsed) ? parsed : [parsed];
              for (const cfg of arr) NS.toast(cfg);
            } catch (_) {}
          }).catch(() => {});
        }
        const ct = r.headers.get('content-type') || '';
        const data = ct.indexOf('application/json') >= 0 ? await r.json() : await r.text();
        if (responseSignal) NS.setSignal(responseSignal, data);
        if (closeOnSuccess) dismiss();
        if (resetOnSuccess) node.reset();
        // Open a widget on success (e.g. "save in drawer → open results sheet").
        const openWidgetName = node.getAttribute('data-fui-rpc-open');
        if (openWidgetName) NS.openWidget(openWidgetName);
        // SPA navigate on success.
        const navigatePath = node.getAttribute('data-fui-rpc-navigate');
        if (navigatePath) {
          NS.navigate(navigatePath);
        }
      } catch (err) {
        // Network error: write human-readable feedback to the signal.
        if (responseSignal) {
          NS.setSignal(responseSignal, { ok: false, status: 0, text: 'Network error \u2014 please try again' });
        }
      } finally {
        if (node.tagName === 'BUTTON' || node.tagName === 'INPUT') node.disabled = false;
        // Task C: remove fui-loading CSS class and aria-busy after RPC completes.
        node.classList.remove('fui-loading');
        node.removeAttribute('aria-busy');
      }
    }

    // data-fui-fill-input click delegator.
    widgetEl.addEventListener('click', (e) => {
      const btn = e.target.closest('[data-fui-fill-input]');
      if (!btn || !widgetEl.contains(btn)) return;
      const sel = btn.getAttribute('data-fui-fill-input');
      if (!sel) return;
      const target = widgetEl.querySelector(sel) || document.querySelector(sel);
      if (!target) return;
      e.preventDefault();
      const explicit = btn.getAttribute('data-fui-fill-text');
      target.value = explicit !== null ? explicit : btn.textContent.trim();
      target.dispatchEvent(new Event('input', { bubbles: true }));
      try { target.focus(); target.select?.(); } catch (_) {}
    });

    // data-fui-shortcut-click document-level delegator (idempotent).
    if (!document.__fuiShortcutClick) {
      document.__fuiShortcutClick = true;
      document.addEventListener('keydown', (e) => {
        const a = document.activeElement;
        if (a && (a.tagName === 'INPUT' || a.tagName === 'TEXTAREA' || a.isContentEditable)) return;
        if (e.isComposing) return;
        const targets = document.querySelectorAll('[data-fui-shortcut-click]');
        for (const el of targets) {
          const combo = el.getAttribute('data-fui-shortcut-click') || '';
          if (!combo) continue;
          const match = NS._parseCombo(combo);
          if (!match.key) continue;
          if (e.key.toLowerCase() !== match.key) continue;
          if (match.mod && !(e.metaKey || e.ctrlKey)) continue;
          if (match.shift && !e.shiftKey) continue;
          if (match.alt && !e.altKey) continue;
          e.preventDefault();
          try { el.click(); } catch (_) {}
          return;
        }
      });
    }

    // data-fui-shortcut-focus per-element bind.
    widgetEl.querySelectorAll('[data-fui-shortcut-focus]').forEach((el) => {
      const combo = el.getAttribute('data-fui-shortcut-focus') || '';
      if (!combo) return;
      const match = NS._parseCombo(combo);
      document.addEventListener('keydown', (e) => {
        if (!match.key) return;
        if (e.key.toLowerCase() !== match.key) return;
        if (match.mod && !(e.metaKey || e.ctrlKey)) return;
        if (match.shift && !e.shiftKey) return;
        if (match.alt && !e.altKey) return;
        if (e.isComposing) return;
        e.preventDefault();
        try { el.focus(); el.select?.(); } catch (_) {}
      });
    });

    // data-fui-tick-elapsed setInterval (rounds the count up by 200ms).
    const tickElapsed = () => {
      widgetEl.querySelectorAll('[data-fui-tick-elapsed]').forEach((el) => {
        const start = parseInt(el.getAttribute('data-fui-tick-elapsed'), 10);
        if (!start) return;
        const ms = Date.now() - start;
        let txt;
        if (ms < 1000) txt = ms + 'ms';
        else if (ms < 10000) txt = (ms / 1000).toFixed(1) + 's';
        else txt = Math.round(ms / 1000) + 's';
        el.textContent = txt;
      });
    };
    tickElapsed();
    setInterval(tickElapsed, 200);

    // textarea[data-fui-autogrow].
    widgetEl.querySelectorAll('textarea[data-fui-autogrow]').forEach((ta) => {
      const grow = () => {
        ta.style.height = 'auto';
        ta.style.height = ta.scrollHeight + 'px';
      };
      ta.addEventListener('input', grow);
      const form = ta.form;
      if (form) form.addEventListener('reset', () => requestAnimationFrame(grow));
      Promise.resolve().then(grow);
    });

    // data-fui-persist-storage.
    widgetEl.querySelectorAll('[data-fui-persist-storage]').forEach((el) => {
      const key = el.getAttribute('data-fui-persist-storage');
      if (!key) return;
      try {
        const saved = window.localStorage.getItem(key);
        if (saved && !el.value) {
          el.value = saved;
          el.dispatchEvent(new Event('input', { bubbles: true }));
        }
      } catch (_) {}
      el.addEventListener('input', () => {
        try { window.localStorage.setItem(key, el.value); } catch (_) {}
      });
      const form = el.form;
      if (form) form.addEventListener('reset', () => {
        try { window.localStorage.removeItem(key); } catch (_) {}
      });
    });

    // (data-fui-copy-text-from is now globally delegated in core
    // runtime.js — see _installGlobalCopyHandler. The previous
    // widget-scoped listener only fired for copy buttons inside a
    // mounted widget, which stranded standalone framework/ui.CopyButton
    // instances anywhere else on the page.)

    // data-fui-charcount-source.
    widgetEl.querySelectorAll('[data-fui-charcount-source]').forEach((el) => {
      const sel = el.getAttribute('data-fui-charcount-source');
      if (!sel) return;
      const src = widgetEl.querySelector(sel) || document.querySelector(sel);
      if (!src) return;
      const sync = () => { el.textContent = src.value.length + ' chars'; };
      src.addEventListener('input', sync);
      const form = src.form;
      if (form) form.addEventListener('reset', () => requestAnimationFrame(sync));
      sync();
    });

    // data-fui-clear-on-esc.
    widgetEl.querySelectorAll('[data-fui-clear-on-esc]').forEach((el) => {
      el.addEventListener('keydown', (e) => {
        if (e.key !== 'Escape' || !el.value) return;
        e.preventDefault();
        e.stopPropagation();
        el.value = '';
        el.dispatchEvent(new Event('input', { bubbles: true }));
      });
    });

    // form[data-fui-submit-on-enter].
    const enterForms = widgetEl.querySelectorAll('form[data-fui-submit-on-enter]');
    const isEnter = (e) => (e.key === 'Enter' || e.code === 'Enter' || e.keyCode === 13);
    enterForms.forEach((form) => {
      form.querySelectorAll('textarea').forEach((ta) => {
        ta.addEventListener('keydown', (e) => {
          if (!isEnter(e) || e.shiftKey || e.isComposing) return;
          e.preventDefault();
          e.stopPropagation();
          if (typeof form.requestSubmit === 'function') {
            form.requestSubmit();
          } else {
            form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
          }
        });
        ta.addEventListener('keypress', (e) => {
          if (!isEnter(e) || e.shiftKey) return;
          e.preventDefault();
          e.stopPropagation();
        });
      });
    });

    // form[data-fui-disable-when-invalid].
    const validityForms = widgetEl.querySelectorAll('form[data-fui-disable-when-invalid]');
    validityForms.forEach((form) => {
      const sync = () => {
        const ok = form.checkValidity();
        form.querySelectorAll('button[type="submit"], input[type="submit"]').forEach((btn) => {
          btn.disabled = !ok;
        });
      };
      form.addEventListener('input', sync);
      form.addEventListener('change', sync);
      form.addEventListener('reset', () => { requestAnimationFrame(sync); });
      Promise.resolve().then(sync);
    });

    // Widget-scoped click + submit.
    widgetEl.addEventListener('click', async (e) => {
      const btn = e.target.closest('[data-fui-rpc]');
      if (btn && widgetEl.contains(btn) && btn.tagName !== 'FORM') {
        e.preventDefault();
        await dispatchRPC(btn);
        return;
      }
      const closeBtn = e.target.closest('[data-fui-action="close"]');
      if (closeBtn && widgetEl.contains(closeBtn)) {
        e.preventDefault();
        dismiss();
      }
    });
    widgetEl.addEventListener('submit', async (e) => {
      const form = e.target.closest('form[data-fui-rpc]');
      if (form && widgetEl.contains(form)) {
        e.preventDefault();
        await dispatchRPC(form);
      }
    });

    if (cfg.closeOnClick && backdrop) backdrop.addEventListener('click', dismiss);
    if (isModal) NS._widgets[cfg.name].closeOnEscape = !!cfg.closeOnEscape;

    if (!isModal && (cfg.closeOnEscape || cfg.closeOnClick)) {
      NS._widgets[cfg.name].closeOnEscape = !!cfg.closeOnEscape;
      NS._widgets[cfg.name].closeOnClickOutside = !!cfg.closeOnClick;
      (NS._popoverStack ||= []).push(cfg.name);
      if (cfg.closeOnClick) {
        const outsideHandler = (e) => {
          if (widgetEl.contains(e.target)) return;
          const trigger = e.target.closest('[data-fui-open="' + cfg.name + '"]');
          if (trigger) return;
          dismiss();
        };
        NS._widgets[cfg.name].outsideHandler = outsideHandler;
        setTimeout(() => document.addEventListener('click', outsideHandler), 0);
      }
    }

    // Global click+submit dispatcher (idempotent across widgets +
    // mount calls). Handles legacy data-kiln-tool + plain forms.
    if (!document.__fuiGlobalDispatch) {
      document.__fuiGlobalDispatch = true;
      document.addEventListener('click', async (e) => {
        if (e.target.closest('[data-fui-widget]')) return;
        const fuiBtn = e.target.closest('[data-fui-rpc]');
        if (fuiBtn && fuiBtn.tagName !== 'FORM') { e.preventDefault(); await dispatchRPC(fuiBtn); return; }
        // Scoped to kiln-rendered pages (body.kiln-app) or trusted
        // subtrees — see runtime.js for the threat model.
        const legacy = e.target.closest('[data-kiln-tool]');
        if (legacy && (document.body.classList.contains('kiln-app') ||
                       legacy.closest('[data-fui-trusted]'))) {
          e.preventDefault();
          const tool = legacy.getAttribute('data-kiln-tool');
          const args = legacy.getAttribute('data-kiln-args') || '';
          // CSRF: forward <meta name="csrf-token"> so the auth.CSRF
          // middleware accepts this state-changing tool invocation. The
          // JSON body can't carry the urlencoded `_csrf` field the
          // middleware parses, so the header channel is required.
          const ktHeaders = { 'Content-Type': 'application/json' };
          const ktMeta = document.querySelector('meta[name="csrf-token"]');
          if (ktMeta) {
            const ktTok = ktMeta.getAttribute('content');
            if (ktTok) ktHeaders['X-CSRF-Token'] = ktTok;
          }
          try {
            await fetch('/kiln/tool/' + tool, {
              method: 'POST',
              credentials: 'same-origin',
              headers: ktHeaders,
              body: args,
            });
          } catch (_) {}
        }
      });
      // Note: the document-level `submit` dispatcher previously lived
      // here AND in runtime.js. Two installations of the same handler
      // drifted in this very PR (bare `navigate()` vs `window.__gofastr?.navigate()`).
      // The dispatcher now lives ONLY in runtime.js — both files share
      // the `document.__fuiGlobalDispatch` guard so whichever loads
      // first wins, and the click handler above remains here for the
      // widget-loading-without-runtime case. See forms_dedup_test.go.
    }
  };

  // Modal-stack Escape close. Modals take priority over non-modal
  // popovers; both stacks are LIFO so nested surfaces unwind in
  // order. Installed once at module load — idempotent flag prevents
  // double-binding if widgets.js gets re-loaded for any reason.
  if (!document.__fuiModalEsc) {
    document.__fuiModalEsc = true;
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Escape') return;
      const G = window.__gofastr;
      if (G._modalStack && G._modalStack.length > 0) {
        const topName = G._modalStack[G._modalStack.length - 1];
        const top = G._widgets[topName];
        if (top && top.closeOnEscape) {
          e.stopPropagation();
          G.closeWidget(topName);
          return;
        }
      }
      if (G._popoverStack && G._popoverStack.length > 0) {
        const topName = G._popoverStack[G._popoverStack.length - 1];
        const top = G._widgets[topName];
        if (top && top.closeOnEscape) {
          e.stopPropagation();
          G.closeWidget(topName);
        }
      }
    });
  }

  // Modal Tab focus trap. Cycles focus within the topmost modal.
  if (!document.__fuiModalTab) {
    document.__fuiModalTab = true;
    document.addEventListener('keydown', (e) => {
      if (e.key !== 'Tab') return;
      const G = window.__gofastr;
      if (!G._modalStack || G._modalStack.length === 0) return;
      const topName = G._modalStack[G._modalStack.length - 1];
      const root = G._widgets[topName]?.root;
      if (!root) return;
      const focusables = Array.from(root.querySelectorAll(G._focusSel))
        .filter((el) => el.offsetParent !== null || el === document.activeElement);
      if (focusables.length === 0) return;
      const first = focusables[0], last = focusables[focusables.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault(); last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault(); first.focus();
      } else if (!root.contains(document.activeElement)) {
        e.preventDefault(); first.focus();
      }
    }, true);
  }

  (NS.loadedModules ||= {}).widgets = true;
})();
