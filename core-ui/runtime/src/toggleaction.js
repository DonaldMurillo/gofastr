// ToggleAction runtime — the three-state cousin of optimisticaction.
// optimisticaction commits once and stays committed; ToggleAction
// supports two additional UX patterns that the existing widget
// explicitly blocked (V3 #10):
//
//   1. data-fui-toggle-group="<key>"  — buttons sharing the same group
//      key form a mutex. Committing one auto-revokes any sibling that
//      was previously committed (no extra RPC, the server is the
//      source of truth; the UI flips optimistically and a subsequent
//      navigate refreshes from server state).
//
//   2. data-fui-toggle-allow-untoggle="true" — clicking an already-
//      committed button reverts it to idle. POSTs the untoggle
//      endpoint (data-fui-toggle-untoggle-endpoint) if set, otherwise
//      just flips locally. Without this, the button is sticky once
//      committed, matching optimisticaction's behaviour.
//
// Each <button data-fui-comp="ui-toggle-action"> declares its commit
// endpoint via data-fui-toggle-endpoint and (optionally) method via
// data-fui-toggle-method (defaults POST). SSR ships the initial state
// in data-state="idle|committed". The widget renders two child spans
// (data-fui-toggle-idle / data-fui-toggle-committed) — the runtime
// shows/hides via the [hidden] attribute, no inline CSS required.
(() => {
  'use strict';

  const setState = (btn, state) => {
    btn.setAttribute('data-state', state);
    const idle = btn.querySelector('[data-fui-toggle-idle]');
    const committed = btn.querySelector('[data-fui-toggle-committed]');
    if (!idle || !committed) return;
    if (state === 'committed' || state === 'pending') {
      idle.setAttribute('hidden', '');
      committed.removeAttribute('hidden');
    } else {
      idle.removeAttribute('hidden');
      committed.setAttribute('hidden', '');
    }
    if (state === 'pending') {
      btn.setAttribute('aria-busy', 'true');
      btn.disabled = true;
    } else {
      btn.removeAttribute('aria-busy');
      btn.disabled = false;
    }
    // aria-pressed mirrors the binary toggle state for AT users — the
    // "committed" state IS the pressed-toggle convention.
    btn.setAttribute('aria-pressed', state === 'committed' ? 'true' : 'false');
  };

  const csrfHeaders = () => {
    const headers = { Accept: 'application/json' };
    const meta = document.querySelector('meta[name="csrf-token"]');
    if (meta) {
      const tok = meta.getAttribute('content');
      if (tok) headers['X-CSRF-Token'] = tok;
    }
    return headers;
  };

  const fireAndForget = (url, method) => {
    if (!url) return Promise.resolve(true);
    return fetch(url, {
      method: (method || 'POST').toUpperCase(),
      credentials: 'same-origin',
      headers: csrfHeaders(),
    }).then((res) => res.ok, () => false);
  };

  const revokeGroupSiblings = (group, except) => {
    if (!group) return;
    // CSS.escape: a quote/backslash in the group key must not blow up
    // the selector (an unescaped throw here would strand the clicked
    // button in its pending/disabled state).
    const sel = `[data-fui-comp="ui-toggle-action"][data-fui-toggle-group="${CSS.escape(group)}"]`;
    for (const other of document.querySelectorAll(sel)) {
      if (other === except) continue;
      if (other.getAttribute('data-state') === 'committed') {
        setState(other, 'idle');
      }
    }
  };

  const setupOne = (btn) => {
    if (btn.__fuiToggleBound) return;
    btn.__fuiToggleBound = true;
    btn.addEventListener('click', (ev) => {
      ev.preventDefault();
      if (btn.getAttribute('data-state') === 'pending') return;
      const state = btn.getAttribute('data-state') || 'idle';
      const group = btn.getAttribute('data-fui-toggle-group') || '';
      const allowUntoggle =
        (btn.getAttribute('data-fui-toggle-allow-untoggle') || '').toLowerCase() === 'true';
      const commitURL = btn.getAttribute('data-fui-toggle-endpoint');
      const untoggleURL = btn.getAttribute('data-fui-toggle-untoggle-endpoint');
      const method = btn.getAttribute('data-fui-toggle-method') || 'POST';

      if (state === 'committed') {
        if (!allowUntoggle) return; // sticky — matches optimisticaction default
        setState(btn, 'pending');
        btn.dispatchEvent(new CustomEvent('toggle-action:untoggle', { bubbles: true }));
        fireAndForget(untoggleURL, method).then((ok) => {
          setState(btn, ok ? 'idle' : 'committed');
        });
        return;
      }

      // state === 'idle' — commit, and revoke any sibling in the group.
      setState(btn, 'pending');
      revokeGroupSiblings(group, btn);
      btn.dispatchEvent(new CustomEvent('toggle-action:commit', { bubbles: true }));
      fireAndForget(commitURL, method).then((ok) => {
        setState(btn, ok ? 'committed' : 'idle');
      });
    });
  };

  const scan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    for (const btn of scope.querySelectorAll('[data-fui-comp="ui-toggle-action"]')) {
      setupOne(btn);
    }
  };

  requestAnimationFrame(() => scan(document));
  document.addEventListener('gofastr:navigate', () => {
    requestAnimationFrame(() => scan(document));
  });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr._moduleScanners = window.__gofastr._moduleScanners || {};
  window.__gofastr._moduleScanners.toggleaction = scan;
  window.__gofastr.toggleaction = { rescan: scan };
})();
