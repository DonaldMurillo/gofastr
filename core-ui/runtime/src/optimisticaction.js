// OptimisticAction runtime — on click, flip the button to its
// committed state IMMEDIATELY (showing the SSR-declared success
// label), then fire the RPC. On non-2xx (or network error) revert to
// idle and play a small shake animation.
//
// Loaded on-demand when a [data-fui-comp="ui-optimistic-action"] element appears.
(() => {
  'use strict';

  const setState = (btn, state) => {
    btn.setAttribute('data-state', state);
    const idle = btn.querySelector('[data-fui-optimistic-idle]');
    const success = btn.querySelector('[data-fui-optimistic-success]');
    if (!idle || !success) return;
    if (state === 'committed' || state === 'pending') {
      idle.setAttribute('hidden', '');
      success.removeAttribute('hidden');
    } else { // idle or error
      idle.removeAttribute('hidden');
      success.setAttribute('hidden', '');
    }
    // A11y + duplicate-submit defense: only pending blocks input. The
    // committed state is final but the button stays focusable so apps
    // can attach an "undo" UI by replacing the DOM. Error / idle clear
    // everything.
    if (state === 'pending') {
      btn.setAttribute('aria-busy', 'true');
      btn.disabled = true;
    } else {
      btn.removeAttribute('aria-busy');
      btn.disabled = false;
    }
  };

  const setupOne = (btn) => {
    if (btn.__fuiOptimisticBound) return;
    btn.__fuiOptimisticBound = true;
    btn.addEventListener('click', (ev) => {
      ev.preventDefault();
      // Already committed → no-op. Apps that want toggle behavior
      // should listen to the optimistic-action:committed event below
      // and replace the button DOM with a new instance.
      const state = btn.getAttribute('data-state');
      if (state === 'committed' || state === 'pending') return;
      // A new click cancels any pending rollback timer scheduled by
      // the previous error path. Without this, an error → user-clicks-
      // again → pending → 600ms timer-fires → clobbers state back to
      // idle while the new fetch is still in flight.
      if (btn.__fuiRollbackTimer) {
        clearTimeout(btn.__fuiRollbackTimer);
        btn.__fuiRollbackTimer = null;
      }
      const url = btn.getAttribute('data-fui-optimistic-endpoint');
      const method = (btn.getAttribute('data-fui-optimistic-method') || 'POST').toUpperCase();
      if (!url) return;
      // Optimistic flip — paint success state on the next frame.
      setState(btn, 'pending');
      // Dispatch a custom event so apps can hook in (e.g. update an
      // adjacent counter, swap an icon).
      btn.dispatchEvent(new CustomEvent('optimistic-action:start', { bubbles: true }));

      // CSRF: forward the page's <meta name="csrf-token"> as
      // X-CSRF-Token on every state-changing fetch. Apps are
      // responsible for verifying the token server-side; the runtime
      // just makes the value available without each call site
      // remembering it.
      const headers = { Accept: 'application/json' };
      const tokenMeta = document.querySelector('meta[name="csrf-token"]');
      if (tokenMeta) {
        const token = tokenMeta.getAttribute('content');
        if (token) headers['X-CSRF-Token'] = token;
      }
      fetch(url, { method, credentials: 'same-origin', headers })
        .then((res) => {
          if (res.ok) {
            setState(btn, 'committed');
            btn.dispatchEvent(new CustomEvent('optimistic-action:committed', { bubbles: true }));
            return;
          }
          throw new Error('non-2xx');
        })
        .catch(() => {
          setState(btn, 'error');
          // After the shake animation finishes (~400ms), revert to idle.
          // Stored on the button so a new click can cancel it (see the
          // guard above).
          btn.__fuiRollbackTimer = setTimeout(() => {
            btn.__fuiRollbackTimer = null;
            setState(btn, 'idle');
          }, 600);
          btn.dispatchEvent(new CustomEvent('optimistic-action:rolled-back', { bubbles: true }));
        });
    });
  };

  const scan = (root) => {
    const scope = root && root.querySelectorAll ? root : document;
    for (const btn of scope.querySelectorAll('[data-fui-comp="ui-optimistic-action"]')) {
      setupOne(btn);
    }
  };

  requestAnimationFrame(() => scan(document));
  document.addEventListener('gofastr:navigate', () => {
    requestAnimationFrame(() => scan(document));
  });

  window.__gofastr = window.__gofastr || {};
  window.__gofastr.optimisticaction = { rescan: scan };
})();
