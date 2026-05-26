// Copy-to-clipboard runtime module.
//
// Loaded on-demand when a [data-fui-copy-text-from] marker is on the
// page (or arrives via SPA-nav). The handler delegates from document,
// so a single listener covers every copy button on the page.
//
// Triggers:
//   data-fui-copy-text-from="<selector>"   the element whose text to copy
//
// Feedback channels:
//   - Adds `.fui-copied` to the button for 1.2s (CSS swaps the inner
//     `.ui-copy-btn__label` ↔ `.ui-copy-btn__copied` spans).
//   - If the button has a sibling/ancestor [data-fui-copy-status],
//     writes the configured text into it (polite aria-live region).
//     data-fui-copy-announce overrides the default "Copied" string.
//   - If data-fui-copy-toast is set (JSON config), dispatches a toast
//     via window.__gofastr.toast({...}). Loads the toasts module on
//     demand if it isn't already present.
//
// No server round-trip — clipboard write is client-only.

(function () {
  'use strict';

  document.addEventListener('click', function (e) {
    const btn = e.target && e.target.closest && e.target.closest('[data-fui-copy-text-from]');
    if (!btn) return;
    const sel = btn.getAttribute('data-fui-copy-text-from');
    if (!sel) return;
    const target = document.querySelector(sel);
    if (!target) return;
    e.preventDefault();
    const text = (target.innerText || target.textContent || '').trim();

    const flash = () => {
      btn.classList.add('fui-copied');
      setTimeout(() => btn.classList.remove('fui-copied'), 1200);
    };
    const announce = () => {
      const root = btn.parentElement || btn;
      const status = root.querySelector('[data-fui-copy-status]')
        || btn.querySelector('[data-fui-copy-status]');
      if (!status) return;
      const msg = btn.getAttribute('data-fui-copy-announce') || 'Copied';
      status.textContent = '';
      setTimeout(() => { status.textContent = msg; }, 30);
    };
    const fireToast = () => {
      const raw = btn.getAttribute('data-fui-copy-toast');
      if (!raw) return;
      try {
        const cfg = JSON.parse(raw);
        if (window.__gofastr && window.__gofastr.toast) {
          window.__gofastr.toast(cfg);
        } else if (window.__gofastr && window.__gofastr.loadModule) {
          // Toast stack might not be loaded yet — load on demand.
          window.__gofastr.loadModule('toasts').then(() => {
            if (window.__gofastr.toast) window.__gofastr.toast(cfg);
          }).catch(() => {});
        }
      } catch (_) { /* malformed JSON: ignore */ }
    };
    const success = () => { flash(); announce(); fireToast(); };

    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(success, success);
    } else {
      try {
        const ta = document.createElement('textarea');
        ta.value = text;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        ta.remove();
        success();
      } catch (_) { /* deliberately silent — copy is best-effort */ }
    }
  });

  // Self-register as loaded so the marker scanner skips re-fetching.
  window.__gofastr = window.__gofastr || {};
  (window.__gofastr.loadedModules ||= {}).copy = true;
})();
