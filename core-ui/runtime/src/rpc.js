// GoFastr runtime module, RPC dispatch
//
// Core owns the document click, submit, and input bridges so an interaction
// that lands before this file arrives is retained. This module owns request
// construction, response effects, abort deduplication, and kiln POSTs. It
// registers no document listeners; every entry comes through dispatchRPC.
(() => {
  'use strict';
  window.__gofastr = window.__gofastr || {};
  const NS = window.__gofastr;

  // Per-signal abort controllers make rapid clicks last-write-wins. A new
  // dispatch aborts the prior request targeting the same signal so network
  // response order cannot roll pagination or filtering back.
  const _rpcInFlight = new Map();
  // Input-triggered RPC debounce belongs with dispatch state, not the core
  // bridge. The bridge loads this module on the first input and asks this map
  // to schedule the request.
  const _idt = new WeakMap();

  function _csrf(headers) {
    const token = document.querySelector('meta[name="csrf-token"]')?.content;
    if (token) headers['X-CSRF-Token'] = token;
    return headers;
  }

  const _kilnOK = (el) =>
    document.body.classList.contains('kiln-app') || el.closest('[data-fui-trusted]');

  const _kilnPost = (el, body) => {
    // The tool name is DOM-borne (data-kiln-tool) and lands in a URL
    // PATH: without a shape gate a "../../admin/…" value normalizes and
    // re-targets the POST onto any same-origin route, carrying the
    // page's CSRF token. Kiln emits tool names as plain identifiers, so
    // the anchored [A-Za-z0-9_-] class rejects nothing legitimate — the
    // same posture as loadModule's name gate in boot.js.
    const tool = el.getAttribute('data-kiln-tool') || '';
    if (!/^[A-Za-z0-9_-]+$/.test(tool)) return Promise.resolve();
    return fetch('/kiln/tool/' + tool, {
      method: 'POST',
      headers: _csrf({ 'Content-Type': 'application/json' }),
      body,
    }).catch(() => {});
  };

  async function _dispatchKiln(node) {
    if (!_kilnOK(node)) return;
    let body = node.getAttribute('data-kiln-args') || '';
    if (node.tagName === 'FORM') {
      const obj = {};
      new FormData(node).forEach((v, k) => { obj[k] = v; });
      body = JSON.stringify(obj);
    } else if (!body && node.form) {
      // A form control that belongs to a form (radio/select/input/textarea
      // with node.form set) carries its value by serializing the enclosing
      // form, the same class as the data-fui-rpc fix below. Explicit
      // data-kiln-args wins (read above); a control with no form keeps the
      // legacy '' body rather than erroring.
      const obj = {};
      new FormData(node.form).forEach((v, k) => { obj[k] = v; });
      body = JSON.stringify(obj);
    }
    await _kilnPost(node, body);
  }

  async function _dispatchPlainForm(form) {
    const action = form.getAttribute('action');
    // Re-check in the module even though core checks before preventing the
    // native submit. The DOM can change while the script is downloading.
    if (!action || !NS._sameOrigin(action)) return;
    const enctype = (form.getAttribute('enctype') || '').toLowerCase();
    const wantsJSON = enctype === 'application/json';
    const explicitSPA = form.hasAttribute('data-fui-spa');
    if (!wantsJSON && !explicitSPA) return;

    const wantsForm = enctype === 'application/x-www-form-urlencoded' ||
                      enctype === 'multipart/form-data';
    const fd = new FormData(form);
    let body, headers;
    if (wantsJSON) {
      const obj = {};
      fd.forEach((v, k) => { obj[k] = v; });
      body = JSON.stringify(obj);
      headers = { 'Content-Type': 'application/json' };
    } else if (wantsForm) {
      if (enctype === 'multipart/form-data') {
        body = fd;
        headers = {};
      } else {
        const params = new URLSearchParams();
        fd.forEach((v, k) => params.append(k, v));
        body = params;
        headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
      }
    } else {
      // data-fui-spa with no enctype defaults to urlencoded so r.ParseForm()
      // sees the same shape as a native form submit.
      const params = new URLSearchParams();
      fd.forEach((v, k) => params.append(k, v));
      body = params;
      headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
    }

    try {
      const resp = await fetch(action, {
        method: form.getAttribute('method') || 'POST',
        headers,
        body,
        redirect: 'follow',
        credentials: 'same-origin',
      });
      if (resp.redirected && resp.url) {
        // A hard navigation preserves the original form contract: the target
        // may not be in the SPA route table, and rebuilding also resets SSE.
        window.location.assign(resp.url);
      }
    } catch (err) {
      // The write may have committed. Surface the failure so the user does not
      // press submit again and duplicate it.
      console.error('[gofastr] form submit could not complete', err);
      if (typeof NS.toast === 'function') {
        NS.toast({ variant: 'error', title: 'Could not complete that submission.', ttl: 6000 });
      }
    }
  }

  async function _dispatchRPC(node, opts) {
    const path = node.getAttribute('data-fui-rpc');
    const method = (node.getAttribute('data-fui-rpc-method') || 'POST').toUpperCase();
    const responseSignal = node.getAttribute('data-fui-rpc-signal');
    const closeOnSuccess = node.hasAttribute('data-fui-rpc-close');
    const resetOnSuccess = node.hasAttribute('data-fui-rpc-reset') && node.tagName === 'FORM';

    // Confirm before touching abort state. Canceling must not abort an older
    // request or leave an unused controller in the per-signal map.
    // opts.confirmed === true means the caller (a submit bridge) already ran
    // the gate on this submit; skip so the user is not prompted twice.
    const confirmMsg = node.getAttribute('data-fui-confirm');
    if (confirmMsg && !(opts && opts.confirmed === true) && typeof window.confirm === 'function') {
      if (!window.confirm(confirmMsg)) return;
    }

    if (responseSignal) {
      const prev = _rpcInFlight.get(responseSignal);
      if (prev) prev.abort();
    }
    const ctl = new AbortController();
    if (responseSignal) _rpcInFlight.set(responseSignal, ctl);

    let body = node.getAttribute('data-fui-rpc-body');
    let resolvedPath = path;
    let bodyIsFormData = false;
    // A form control that belongs to a form (every radio/input/select/
    // textarea with node.form set) carries its value exactly like a FORM
    // node: serialize the enclosing form so the handler sees name=value.
    // An explicit data-fui-rpc-body (read above) still wins; a control with
    // no enclosing form and no explicit body keeps the legacy empty body.
    const formSource = node.tagName === 'FORM' ? node : (node.form || null);
    // A non-form carrier can still own form controls: the combobox
    // renders a <div> carrier so an embedding host <form> survives HTML
    // parsing. Serialize its named, enabled controls exactly like a
    // form so the handler sees name=value; a carrier with no named
    // controls (a plain RPC button) keeps the legacy empty body.
    let fd = null;
    if (!body && formSource) {
      fd = new FormData(formSource);
    } else if (!body && !formSource) {
      const controls = node.querySelectorAll('input[name]:not([type=file]),select[name],textarea[name]');
      if (controls.length) {
        fd = new FormData();
        for (const c of controls) {
          if (c.disabled) continue;
          if ((c.type === 'checkbox' || c.type === 'radio') && !c.checked) continue;
          if (c.tagName === 'SELECT' && c.multiple) {
            for (const o of c.selectedOptions) fd.append(c.name, o.value);
            continue;
          }
          fd.append(c.name, c.value);
        }
      }
    }
    if (fd) {
      if (method === 'GET') {
        const params = new URLSearchParams();
        fd.forEach((v, k) => { if (v != null) params.append(k, String(v)); });
        const qs = params.toString();
        if (qs) resolvedPath = path + (path.includes('?') ? '&' : '?') + qs;
      } else if (formSource && (formSource.enctype === 'multipart/form-data' || formSource.querySelector('input[type="file"]'))) {
        body = fd;
        bodyIsFormData = true;
      } else {
        // Repeated keys become arrays; one value stays scalar. getAll avoids
        // inherited names such as constructor and toString on the plain object.
        const obj = {};
        for (const k of fd.keys()) {
          const v = fd.getAll(k);
          obj[k] = v.length > 1 ? v : v[0];
        }
        body = JSON.stringify(obj);
      }
    }

    const widgetEl = node.closest('[data-fui-widget]');
    const widgetName = (widgetEl && widgetEl.getAttribute('data-fui-widget')) || '';
    const wentry = widgetName && NS._widgets
      && Object.prototype.hasOwnProperty.call(NS._widgets, widgetName)
      && NS._widgets[widgetName];
    const headers = _csrf({});
    if (widgetName) headers['X-FUI-Widget'] = widgetName;
    if (body && !bodyIsFormData) headers['Content-Type'] = 'application/json';

    // Signal-targeted requests stay clickable because their abort controller
    // makes rapid replacement safe. Other button/input triggers are disabled.
    const wantDisable = !responseSignal && (node.tagName === 'BUTTON' || node.tagName === 'INPUT');
    if (wantDisable) node.disabled = true;
    node.classList.add('fui-loading');
    node.setAttribute('aria-busy', 'true');
    try {
      if (!NS._originOK(resolvedPath)) return;
      const r = await fetch(resolvedPath, {
        method,
        headers,
        body: body || undefined,
        signal: ctl.signal,
        credentials: 'same-origin',
      });
      if (!r.ok) {
        const txt = await r.text();
        if (responseSignal) NS.setSignal(responseSignal, { ok: false, status: r.status, text: txt });
        return;
      }

      // nav is absent from the embed composition, so invalidation is optional.
      NS._inval?.(r);
      const pushState = r.headers.get('X-Gofastr-Push-State') ||
        node.getAttribute('data-fui-push-state');
      if (pushState) {
        try {
          // Through the router's choke point (entry id + currentPath).
          // nav is absent from the embed composition, so fall back to a
          // raw write there, an embed has no router state to keep.
          if (NS._pushURL) NS._pushURL(pushState);
          else {
            history.pushState(null, '', pushState);
            NS._setCurrentPath(location.pathname + location.search);
          }
        } catch (_) {}
      }

      const toastHeader = r.headers.get('X-Gofastr-Toast');
      if (toastHeader) NS._dispatchToastHeader(toastHeader);
      const ct = r.headers.get('content-type') || '';
      const data = ct.indexOf('application/json') >= 0 ? await r.json() : await r.text();
      if (responseSignal) NS.setSignal(responseSignal, data);
      if (closeOnSuccess && wentry && wentry.dismiss) wentry.dismiss();
      if (resetOnSuccess) node.reset();

      // data-fui-rpc-after-done is written through dataset so repeated clicks
      // cannot reapply one-shot text or disabled state.
      if (!node.dataset.fuiRpcAfterDone) {
        const afterText = node.getAttribute('data-fui-rpc-after-text');
        if (afterText !== null) node.textContent = afterText;
        if (node.hasAttribute('data-fui-rpc-after-disable')) {
          node.setAttribute('aria-disabled', 'true');
          if ('disabled' in node) node.disabled = true;
        }
        node.dataset.fuiRpcAfterDone = '1';
      }
      const scrollSel = node.getAttribute('data-fui-rpc-scroll-to');
      if (scrollSel) {
        // The hint must never corrupt the result: this lookup runs after
        // the response signal is set, in its own try, so a malformed
        // selector degrades to a no-op instead of landing in the outer
        // catch that would overwrite the signal with the network-error
        // object.
        try {
          const target = document.querySelector(scrollSel);
          if (target) Promise.resolve().then(() => {
            try { target.scrollIntoView({ behavior: 'smooth', block: 'nearest' }); }
            catch (_) {}
          });
        } catch (_) {}
      }

      const refreshName = node.getAttribute('data-fui-rpc-refresh') || widgetName;
      const rentry = NS._widgets
        && Object.prototype.hasOwnProperty.call(NS._widgets, refreshName)
        && NS._widgets[refreshName];
      if (rentry && rentry.pollNow) rentry.pollNow();

      const openWidgetName = node.getAttribute('data-fui-rpc-open');
      if (openWidgetName) {
        NS.loadModule('widgets').then(() => {
          NS.openWidget(openWidgetName);
        }).catch(() => {});
      }

      const navigatePath = node.getAttribute('data-fui-rpc-navigate');
      if (navigatePath) {
        try { NS.navigate(navigatePath, { force: true }); }
        catch (_) {}
      }
    } catch (err) {
      if (err && err.name === 'AbortError') return;
      if (responseSignal) {
        NS.setSignal(responseSignal, {
          ok: false,
          status: 0,
          text: 'Network error \u2014 please try again',
        });
      }
    } finally {
      if (responseSignal && _rpcInFlight.get(responseSignal) === ctl) {
        _rpcInFlight.delete(responseSignal);
      }
      const sticky = node.hasAttribute('data-fui-rpc-after-disable') &&
        node.dataset.fuiRpcAfterDone === '1';
      if (!sticky && wantDisable) node.disabled = false;
      node.classList.remove('fui-loading');
      node.removeAttribute('aria-busy');
    }
  }

  async function dispatchRPC(node, source, opts) {
    if (!node) return;
    if (source === 'input') {
      const ms = parseInt(node.getAttribute('data-fui-rpc-debounce-ms') || '250', 10) || 250;
      const prev = _idt.get(node);
      clearTimeout(prev);
      _idt.set(node, setTimeout(() => {
        _idt.delete(node);
        _dispatchRPC(node);
      }, ms));
      return;
    }
    if (node.hasAttribute('data-kiln-tool')) return _dispatchKiln(node);
    if (!node.hasAttribute('data-fui-rpc')) return _dispatchPlainForm(node);
    return _dispatchRPC(node, opts);
  }

  Object.assign(NS, {
    _csrf,
    dispatchRPC,
  });
  (NS.loadedModules ||= {}).rpc = true;
})();
