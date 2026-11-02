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
  // The latest dispatch per node and per form. A node with no signal has
  // no abort controller, so two quick submits of one form race; an answer
  // to a dispatch its form (or, outside a form, its node) has since
  // superseded is dropped before it touches the page (a late refusal must
  // not paint over a newer success, nor a late success clear a newer
  // refusal). The node's own entry decides only its busy state, so a
  // button superseded by its form's submit still re-enables.
  const _rpcLatest = new WeakMap();
  // Triggers with nothing to supersede them (no response signal, no form)
  // while a dispatch is in flight. A second click on such a node is
  // dropped: a span or link cannot be disabled, and two clicks during the
  // cold module load both reached dispatch, so both POSTs landed.
  const _rpcBusy = new WeakSet();
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
    document.body.classList.contains('kiln-app') || el.closest('[data-cui-trusted]');

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
      // form, the same class as the data-cui-rpc fix below. Explicit
      // data-kiln-args wins (read above); a control with no form keeps the
      // legacy '' body rather than erroring.
      const obj = {};
      new FormData(node.form).forEach((v, k) => { obj[k] = v; });
      body = JSON.stringify(obj);
    }
    await _kilnPost(node, body);
  }


  // formResult tells one shared "changed" state (headless-leaveguard's
  // data-hui-leave-guard hook) whether a form submit committed: ok on a
  // 2xx, ok:false on a refusal, so a refused save puts the dirt back.
  const formResult = (form, ok) => {
    if (form) { try { form.dispatchEvent(new CustomEvent('gofastr:formresult', { bubbles: true, detail: { ok } })); } catch (_) {} }
  };

  async function _dispatchPlainForm(form, opts) {
    const submitter = opts?.submitter;
    const attr = (name) => NS._formAttr(form, submitter, name);
    const action = attr('action') || location.href;
    // Re-check in the module even though core checks before preventing the
    // native submit. The DOM can change while the script is downloading.
    if (!NS._sameOrigin(new URL(action, document.baseURI))) return;
    const enctype = (attr('enctype') || '').toLowerCase();
    const wantsJSON = enctype === 'application/json';
    const explicitSPA = form.hasAttribute('data-cui-spa');
    if (!wantsJSON && !explicitSPA) return;

    const fd = new FormData(form, submitter);
    const params = new URLSearchParams();
    fd.forEach((value, key) => params.append(key, typeof value === 'string' ? value : value.name));
    // A control named "method" shadows form.method. Read attributes instead.
    // Only an explicit GET navigates: a missing method stays the POST this
    // bridge has always sent, so its fields never land in the URL.
    const method = (attr('method') || 'POST').toUpperCase();
    if (method === 'GET') {
      // GET filters are route navigation, not a mutation fetch with a body.
      // Match native GET forms: successful controls replace the action query,
      // repeated names stay repeated, and the action's fragment is retained.
      const dest = new URL(NS._lp?.(action) ?? action, document.baseURI);
      dest.search = params.toString();
      const path = dest.pathname + dest.search + dest.hash;
      // Programmatic navigation replaces the page instead of opening an
      // intercept layer. Ask the existing guard about that discard scope;
      // its dialog can replay this exact destination after Discard.
      const navigate = () => NS.navigate(path);
      if (!NS._leaveGuard || NS._leaveGuard.ok(document, navigate)) navigate();
      return;
    }
    let body, headers;
    if (wantsJSON) {
      const obj = {};
      fd.forEach((v, k) => { obj[k] = v; });
      body = JSON.stringify(obj);
      headers = { 'Content-Type': 'application/json' };
    } else if (enctype === 'multipart/form-data') {
      body = fd;
      headers = {};
    } else {
      // data-cui-spa with no enctype defaults to urlencoded so r.ParseForm()
      // sees the same shape as a native form submit.
      body = params;
      headers = { 'Content-Type': 'application/x-www-form-urlencoded' };
    }
    // The page's token rides the header, as on data-cui-rpc: a JSON body
    // has no field middleware.CSRF reads, and a page may carry only the meta.
    _csrf(headers);

    try {
      const resp = await fetch(NS._lp?.(action) ?? action, {
        method,
        headers,
        body,
        redirect: 'follow',
        credentials: 'same-origin',
      });
      // Reported before the redirect below: a committed form must be
      // clean by the time the page unloads.
      formResult(form, resp.ok);
      if (resp.redirected && resp.url) {
        // A hard navigation preserves the original form contract: the target
        // may not be in the SPA route table, and rebuilding also resets SSE.
        window.location.assign(resp.url);
        return;
      }
      if (!resp.ok) {
        // A refusal is never silent: the server's field errors go in place,
        // or a toast names the status, as on a data-cui-rpc form.
        const txt = await resp.text();
        NS.loadModule('formerrors').then(() => NS._formErrors.report(form, resp.status, txt)).catch(() => {});
        return;
      }
      if (form.querySelector('[aria-invalid="true"]')) {
        NS.loadModule('formerrors').then(() => NS._formErrors.clear(form)).catch(() => {});
      }
      NS._inval?.(resp);
      const toastHeader = resp.headers.get('X-Gofastr-Toast');
      if (toastHeader) NS._dispatchToastHeader(toastHeader);
    } catch (err) {
      // The write may have committed. Surface the failure so the user does not
      // press submit again and duplicate it.
      formResult(form, false);
      console.error('[gofastr] form submit could not complete', err);
      if (typeof NS.toast === 'function') {
        NS.toast({ variant: 'error', title: NS.say('runtime.submit_failed', 'Could not complete that submission.'), ttl: 6000 });
      }
    }
  }


  async function _dispatchRPC(node, opts) {
    const path = node.getAttribute('data-cui-rpc');
    const method = (node.getAttribute('data-cui-rpc-method') || 'POST').toUpperCase();
    const responseSignal = node.getAttribute('data-cui-rpc-signal');
    const closeOnSuccess = node.hasAttribute('data-cui-rpc-close');
    const resetOnSuccess = node.hasAttribute('data-cui-rpc-reset') && node.tagName === 'FORM';
    const errToast = node.getAttribute('data-cui-rpc-error-toast');
    const successToast = node.getAttribute('data-cui-rpc-success-toast');

    // Confirm before touching abort state. Canceling must not abort an older
    // request or leave an unused controller in the per-signal map.
    // opts.confirmed === true means the caller (a submit bridge) already ran
    // the gate on this submit; skip so the user is not prompted twice.
    if (node.getAttribute('data-cui-confirm') && !(opts && opts.confirmed === true)) {
      await NS.loadModule('confirm');
      if (!(await NS.ask(node))) return;
    }

    // A form control that belongs to a form (every radio/input/select/
    // textarea with node.form set) carries its value exactly like a FORM
    // node: serialize the enclosing form so the handler sees name=value.
    // An explicit data-cui-rpc-body (read above) still wins; a control with
    // no enclosing form and no explicit body keeps the legacy empty body.
    const formSource = node.tagName === 'FORM' ? node : (node.form || null);
    // A trigger that carries a value (a form, a control, a carrier of
    // controls) keeps superseding: the newest value must reach the server.
    const guardBusy = !responseSignal && !formSource &&
      !node.matches('input,select,textarea') &&
      !node.querySelector('input[name],select[name],textarea[name]');
    if (guardBusy && _rpcBusy.has(node)) return;

    if (responseSignal) {
      const prev = _rpcInFlight.get(responseSignal);
      if (prev) prev.abort();
    }
    const ctl = new AbortController();
    if (responseSignal) _rpcInFlight.set(responseSignal, ctl);

    let body = node.getAttribute('data-cui-rpc-body');
    let resolvedPath = path;
    const tok = {};
    _rpcLatest.set(node, tok);
    if (formSource) _rpcLatest.set(formSource, tok);
    const stale = () => _rpcLatest.get(formSource || node) !== tok;
    // A retry starts clean (the formerrors module placed the previous
    // attempt's messages; nothing to clear until it has loaded).
    if (formSource && NS._formErrors) NS._formErrors.clear(formSource);
    // A non-form carrier can still own form controls: the combobox
    // renders a <div> carrier so an embedding host <form> survives HTML
    // parsing. Serialize its named, enabled controls exactly like a
    // form so the handler sees name=value; a carrier with no named
    // controls (a plain RPC button) keeps the legacy empty body.
    let fd = null;
    if (!body && formSource) {
      // The pressed submit button's name=value rides along, as on a
      // native submit; FormData throws on a submitter the form does not own.
      const sub = opts?.submitter;
      fd = new FormData(formSource, sub?.form === formSource ? sub : null);
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
      } else {
        // Repeated keys become arrays; one value stays scalar. getAll avoids
        // inherited names such as constructor and toString on the plain object.
        // The one repeated shape that is NOT a list is the HTML checkbox
        // idiom, a hidden input followed by a checkbox of the same name
        // (hidden "false", checkbox "true"): the checkbox overrides the
        // hidden when checked, so the pair is one value, the last one.
        const obj = {};
        for (const k of fd.keys()) {
          const v = fd.getAll(k);
          obj[k] = v.length > 1 && !_hiddenCheckboxPair(formSource, k) ? v : v[v.length - 1];
        }
        body = JSON.stringify(obj);
      }
    }

    const widgetEl = node.closest('[data-cui-widget]');
    const widgetName = (widgetEl && widgetEl.getAttribute('data-cui-widget')) || '';
    const wentry = widgetName && NS._widgets
      && Object.prototype.hasOwnProperty.call(NS._widgets, widgetName)
      && NS._widgets[widgetName];
    // Bodyless writes also need JSON content type; fetch owns multipart boundaries.
    const headers = _csrf(body instanceof FormData || method === 'GET' || method === 'HEAD'
      ? {} : { 'Content-Type': 'application/json' });
    if (widgetName) headers['X-FUI-Widget'] = widgetName;

    // Signal-targeted requests stay clickable because their abort controller
    // makes rapid replacement safe. Other button/input triggers are disabled.
    const wantDisable = !responseSignal && (node.tagName === 'BUTTON' || node.tagName === 'INPUT');
    if (wantDisable) node.disabled = true;
    if (guardBusy) _rpcBusy.add(node);
    node.classList.add('cui-loading');
    node.setAttribute('aria-busy', 'true');
    try {
      if (!NS._originOK(resolvedPath)) return;
      const r = await fetch(NS._lp?.(resolvedPath) ?? resolvedPath, {
        method,
        headers,
        body: body || undefined,
        signal: ctl.signal,
        credentials: 'same-origin',
      });
      if (stale()) return;
      // fetch followed a 3xx (an expired session sends the action to the
      // sign-in page), so this 200 is another URL's answer, not the
      // action's: it did not happen here. No success effect runs and the
      // signal never takes the target's body. A form stays put with what
      // the reader typed (navigating would discard it; a leave guard gets
      // ok:false and keeps the dirt) and says the submit did not complete.
      // Anything else hands the target to the navigator (nav.js
      // _followed: another origin or document loads as one), the way an
      // intercepted overlay hands it a redirect.
      if (r.redirected) {
        if (responseSignal) NS.setSignal(responseSignal, { ok: false, status: 0, text: '' });
        if (formSource) {
          formResult(formSource, false);
          NS._toastOrFallback?.({ variant: 'error', title: NS.say('runtime.submit_failed', 'Could not complete that submission.'), ttl: 6000 });
        } else {
          const to = NS._followed?.(r, '');
          if (to) NS.navigate(to, { force: true });
        }
        return;
      }
      if (!r.ok) {
        const txt = await r.text();
        if (stale()) return;
        if (responseSignal) NS.setSignal(responseSignal, { ok: false, status: r.status, text: txt });
        // A refused form submission is never silent: the formerrors
        // module renders the server's validation envelope into the
        // fields (demand-loaded; the happy path never pays for it).
        if (formSource) {
          formResult(formSource, false);
          NS.loadModule('formerrors')
            .then(() => { if (!stale()) NS._formErrors.report(formSource, r.status, txt); })
            .catch(() => {});
        } else if (errToast !== null) {
          // An opted-in button: the attribute is the title, the server's
          // JSON "error" (textContent in the toast, never markup) the body.
          let msg = '';
          try {
            const d = JSON.parse(txt);
            // CRUD's flat {error: "..."} or core/handler's {error: {message}}.
            const m = d?.error?.message ?? d?.error;
            if (typeof m === 'string') msg = m.slice(0, 300);
          } catch (_) {}
          NS._toastOrFallback?.({ variant: 'error', title: errToast || NS.say('runtime.request_failed_status', 'Request failed ({{status}})', { status: r.status }), body: msg, ttl: 6000 });
        }
        return;
      }
      formResult(formSource, true);
      // A form the server rendered with errors (aria-invalid) clears
      // them on success too, loading the module if no refusal has yet.
      if (formSource && formSource.querySelector('[aria-invalid="true"]')) {
        NS.loadModule('formerrors').then(() => NS._formErrors.clear(formSource)).catch(() => {});
      }

      // nav is absent from the embed composition, so invalidation is optional.
      NS._inval?.(r);
      const pushState = r.headers.get('X-Gofastr-Push-State') ||
        node.getAttribute('data-cui-push-state');
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
      // Checked after the body read and the signal broadcast: a newer
      // dispatch may have started during either, and a signal listener
      // may have started one itself.
      if (stale()) return;
      if (closeOnSuccess && wentry && wentry.dismiss) wentry.dismiss();
      if (resetOnSuccess) node.reset();

      // data-cui-rpc-after-done is written through dataset so repeated clicks
      // cannot reapply one-shot text or disabled state.
      if (!node.dataset.cuiRpcAfterDone) {
        const afterText = node.getAttribute('data-cui-rpc-after-text');
        if (afterText !== null) node.textContent = afterText;
        if (node.hasAttribute('data-cui-rpc-after-disable')) {
          node.setAttribute('aria-disabled', 'true');
          if ('disabled' in node) node.disabled = true;
        }
        node.dataset.cuiRpcAfterDone = '1';
      }
      const scrollSel = node.getAttribute('data-cui-rpc-scroll-to');
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

      const refreshName = node.getAttribute('data-cui-rpc-refresh') || widgetName;
      const rentry = NS._widgets
        && Object.prototype.hasOwnProperty.call(NS._widgets, refreshName)
        && NS._widgets[refreshName];
      if (rentry && rentry.pollNow) rentry.pollNow();

      const openWidgetName = node.getAttribute('data-cui-rpc-open');
      if (openWidgetName) {
        NS.loadModule('widgets').then(() => {
          if (!stale()) NS.openWidget(openWidgetName);
        }).catch(() => {});
      }
      // A success toast fires BEFORE the navigate below: the stack lives
      // outside the swapped region, so the toast outlives the navigation
      // its RPC triggered — a save that lands on the record page still
      // says "Saved". _toastOrFallback keeps it reachable when the
      // feedback module cannot load.
      // Its action (Undo) passes through as the attribute's JSON: the
      // feedback module parses it and puts it on the toast's button.
      if (successToast !== null) await NS._toastOrFallback?.({ variant: 'success', title: successToast || NS.say('runtime.done', 'Done'), ttl: 6000, action: node.getAttribute('data-cui-rpc-success-action') });
      const dest = node.getAttribute('data-cui-rpc-navigate');
      // A save inside an intercept layer returns to the layer (or the
      // page under the stack) it names instead of leaving the stack.
      if (dest && !stale() && !NS._interceptReturn?.(dest, node)) {
        // A mutation leaves the page it ran on stale: after a delete,
        // Back replayed the deleted record from the screen cache.
        // Evict it before leaving; selectors carry no locale prefix
        // (the kernel's _unlocalize strips it).
        if (method !== 'GET') NS.invalidate?.(NS._unlocalize(location.pathname) + location.search);
        try { NS.navigate(dest, { force: true }); }
        catch (_) {}
      }
    } catch (err) {
      if ((err && err.name === 'AbortError') || stale()) return;
      // A form never fails silently: a refusal paints its fields, a
      // redirect toasts, and a fetch that never answered (offline, a
      // dropped connection) toasts too, with or without an opted-in
      // title. The leave guard keeps the edits unsaved.
      if (formSource) formResult(formSource, false);
      if (formSource || errToast !== null) {
        NS._toastOrFallback?.({ variant: 'error', title: errToast || NS.say('runtime.request_failed', 'Request failed'), body: NS.say('runtime.network_error', 'Network error: please try again'), ttl: 6000 });
      }
      if (responseSignal) {
        NS.setSignal(responseSignal, {
          ok: false,
          status: 0,
          text: NS.say('runtime.network_error', 'Network error: please try again'),
        });
      }
    } finally {
      if (responseSignal && _rpcInFlight.get(responseSignal) === ctl) {
        _rpcInFlight.delete(responseSignal);
      }
      if (guardBusy) _rpcBusy.delete(node);
      // A superseded dispatch leaves the busy state to the newer one.
      if (_rpcLatest.get(node) === tok) {
        const sticky = node.hasAttribute('data-cui-rpc-after-disable') &&
          node.dataset.cuiRpcAfterDone === '1';
        if (!sticky && wantDisable) node.disabled = false;
        node.classList.remove('cui-loading');
        node.removeAttribute('aria-busy');
      }
    }
  }

  // _hiddenCheckboxPair reports whether name's controls in form are
  // exactly one hidden input followed by one checkbox: the pair a bool
  // field renders so an unchecked box still submits its value.
  function _hiddenCheckboxPair(form, name) {
    const els = form && form.elements ? form.elements.namedItem(name) : null;
    if (!els || els.length !== 2) return false;
    return els[0].type === 'hidden' && els[1].type === 'checkbox';
  }

  async function dispatchRPC(node, source, opts) {
    if (!node) return;
    // Any dispatch drops the node's pending input timer: an input
    // re-arms it below, and a submit or click sends the newest value
    // now, so the timer would only POST the same form a second time.
    clearTimeout(_idt.get(node));
    if (source === 'input') {
      const ms = parseInt(node.getAttribute('data-cui-rpc-debounce-ms') || '250', 10) || 250;
      _idt.set(node, setTimeout(() => {
        // A form that left the page (a navigation, a swap) is gone
        // with what the reader typed: its stale value must not land
        // on the next page.
        if (node.isConnected) _dispatchRPC(node);
      }, ms));
      return;
    }
    if (node.hasAttribute('data-kiln-tool')) return _dispatchKiln(node);
    if (!node.hasAttribute('data-cui-rpc')) return _dispatchPlainForm(node, opts);
    return _dispatchRPC(node, opts);
  }

  Object.assign(NS, {
    _csrf,
    dispatchRPC,
  });
  (NS.loadedModules ||= {}).rpc = true;
})();
