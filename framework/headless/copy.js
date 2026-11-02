// headless-copy: the behaviour module for the copy control (the
// [data-hui-copy] wrapper ui.CopyButton, CodeBlock's copy and a menu's
// copy row render). Loaded by the kernel on the wrapper's marker, at
// boot, on insertion, or after a client navigation. Its own module, not
// a part of headless-feedback, so the toast runtime's size budget does
// not carry the clipboard; a copy that toasts asks the kernel for that
// runtime (headless-feedback also lists the wrapper among its markers,
// so on a copy page it is usually loaded already).
(() => {
  'use strict';
  const NAME = 'headless-copy';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // The target stays readable and selectable; without script the page
  // promises nothing about the clipboard. The words (button label,
  // copied label, status sentence with {name}, the failure sentence)
  // travel on the wrapper from Strings. Success is the clipboard's
  // answer, not the click: the copied label, the status sentence and
  // the toast wait for writeText to resolve, and a refused or missing
  // clipboard (a permission prompt declined, an insecure origin) says
  // the failure sentence instead.
  function announce(wrap, sentence) {
    const status = wrap.querySelector('[data-hui-copy-status]');
    if (!status) return;
    status.textContent = '';
    requestAnimationFrame(() => { status.textContent = sentence; });
  }
  function copied(wrap, id) {
    wrap.setAttribute('data-hui-copy-state', 'done');
    const btn = wrap.querySelector('[data-hui-copy-label]');
    const fmts = wrap.getAttribute('data-hui-copy-copied') || '';
    if (btn && fmts) btn.textContent = fmts;
    const name = wrap.getAttribute('data-hui-copy-name') || id || '';
    announce(wrap, (wrap.getAttribute('data-hui-copy-sentence') || '').replaceAll('{{name}}', () => name));
    const back = wrap.getAttribute('data-hui-copy-back') || '';
    clearTimeout(wrap.__huiCopyBack);
    wrap.__huiCopyBack = setTimeout(() => {
      wrap.removeAttribute('data-hui-copy-state');
      if (btn && back) btn.textContent = back;
    }, 1200);
    // A toast on copy rides headless-feedback's toast runtime through
    // the kernel's _toastOrFallback: it loads that module when the
    // copy wrapper's marker has not already, and a page with no stack
    // gets the kernel's fallback region. A bare page with no kernel
    // calls NS.toast when the feedback module put one there. The
    // config JSON is read from the wrapper itself (a menu's copy row)
    // or whichever element under it carries it (ui.CopyButton puts it
    // on the button).
    const toastCfg = (wrap.querySelector('[data-hui-copy-toast]') || wrap).getAttribute('data-hui-copy-toast');
    if (toastCfg) {
      try {
        const cfg = JSON.parse(toastCfg);
        NS._toastOrFallback ? NS._toastOrFallback(cfg) : NS.toast?.(cfg);
      } catch (_) {}
    }
  }
  function failed(wrap, id) {
    const name = wrap.getAttribute('data-hui-copy-name') || id || '';
    announce(wrap, (wrap.getAttribute('data-hui-copy-failed') || '').replaceAll('{{name}}', () => name));
  }
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
    let write = null;
    try {
      write = navigator.clipboard && navigator.clipboard.writeText ? navigator.clipboard.writeText(text) : null;
    } catch (_) {
      write = null;
    }
    if (!write || typeof write.then !== 'function') { failed(wrap, id); return; }
    write.then(() => copied(wrap, id), () => failed(wrap, id));
  });
})();
