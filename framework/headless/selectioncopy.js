// headless-selection-copy: the behaviour module for a selection's Copy
// control. Loaded by the kernel on the control's marker, at boot, on
// insertion, or after a client navigation. Its own module, not a part
// of the headless behaviour, so the bulk bar's copy does not spend the
// behaviour's size budget on pages that never draw one.
(() => {
  'use strict';
  const NAME = 'headless-selection-copy';
  const NS = window.__gofastr = window.__gofastr || {};
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  // The checked rows, counted the way the headless behaviour counts
  // them: a select-all box is not a row, nor is a box in a form inside
  // the selection (a cell's inline editor for a yes/no value).
  function checkedRows(sel) {
    const rows = [];
    for (const box of sel.querySelectorAll('input[type="checkbox"]:checked:not([data-hui-table-select-all])')) {
      const f = box.closest('form');
      if (!f || !sel.contains(f)) rows.push(box);
    }
    return rows;
  }

  // A selection's Copy control (data-hui-selection-copy) fetches its URL
  // with one _id per checked row and writes the CSV to the clipboard,
  // then toasts. The URL must be on the page's own origin: markup that
  // names another is ignored, so injected markup cannot fill the
  // clipboard from elsewhere. The clipboard write is handed the pending
  // text at once (ClipboardItem with a promise), which keeps the click's
  // user activation in browsers that demand it.
  function copySelection(btn) {
    const sel = btn.closest('[data-hui-selection]');
    if (!sel) return;
    const ids = checkedRows(sel).map((b) => b.value).filter(Boolean);
    if (!ids.length) return;
    let url;
    try { url = new URL(btn.getAttribute('data-hui-selection-copy') || '', location.href); } catch (_) { return; }
    if (url.origin !== location.origin) return;
    for (const id of ids) url.searchParams.append('_id', id);
    const toast = (cfg) => { if (NS.toast) NS.toast(cfg); };
    const failed = () => toast({ title: btn.getAttribute('data-hui-selection-copy-failed') || '', variant: 'danger', ttl: 6000 });
    const text = fetch(url.href, { credentials: 'same-origin' }).then((r) => {
      if (!r.ok) throw new Error(String(r.status));
      return r.text();
    });
    const clip = navigator.clipboard;
    let done;
    if (clip && clip.write && window.ClipboardItem) {
      done = clip.write([new ClipboardItem({ 'text/plain': text.then((t) => new Blob([t], { type: 'text/plain' })) })]);
    } else if (clip && clip.writeText) {
      done = text.then((t) => clip.writeText(t));
    } else {
      text.catch(() => {});
      failed();
      return;
    }
    const said = (btn.getAttribute('data-hui-selection-copied') || '').replace('{n}', () => String(ids.length));
    done.then(() => toast({ title: said, variant: 'success', ttl: 4000 }), failed);
  }

  document.addEventListener('click', (e) => {
    const copy = e.target.closest?.('[data-hui-selection-copy]');
    if (!copy) return;
    e.preventDefault();
    copySelection(copy);
  });
})();
