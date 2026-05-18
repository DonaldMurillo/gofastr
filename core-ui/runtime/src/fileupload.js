// GoFastr runtime module — FileUpload
//
// Wires every [data-fui-fileupload] zone in a subtree:
//   - drag/drop forwards dropped File objects into the inner
//     <input type="file"> via a DataTransfer + dispatches `change`
//   - on `change`, renders a filename + size list into
//     .ui-fileupload__filename, plus a 96px image thumbnail for
//     the first image in the set
//
// Loads on demand:
//   - core.js scans the page and calls __gofastr.loadModule("fileupload")
//     when [data-fui-fileupload] appears (idle after FCP, or on hover
//     of a data-fui-prefetch="fileupload" trigger).
//   - re-runs on every SPA-nav (`gofastr:navigate`) so swapped-in
//     content gets wired.
//
// Idempotent: zones already wired carry a __fuiWired flag.
(() => {
  'use strict';
  function wireFileUploads(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const zones = scope.querySelectorAll('[data-fui-fileupload]');
    for (const zone of zones) {
      if (zone.__fuiWired) continue;
      zone.__fuiWired = true;
      const input = zone.querySelector('input[type="file"]');
      if (!input) continue;
      const filename = zone.querySelector('.ui-fileupload__filename');

      const fmtBytes = (n) => {
        if (n < 1024) return n + ' B';
        if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
        return (n / (1024 * 1024)).toFixed(2) + ' MB';
      };
      const render = () => {
        if (!filename) return;
        filename.innerHTML = '';
        const files = input.files;
        if (!files || files.length === 0) return;
        filename.classList.add('is-populated');
        // Thumbnail uses the first IMAGE in the set (not necessarily
        // files[0] which could be a non-image like a doc submitted
        // alongside screenshots). One FileReader per pick keeps
        // payload small even with 50 photos.
        const firstImage = Array.from(files).find(f => f.type && f.type.startsWith('image/'));
        if (firstImage) {
          const img = document.createElement('img');
          img.className = 'ui-fileupload__thumb';
          img.alt = '';
          const reader = new FileReader();
          reader.onload = (e) => { img.src = e.target.result; };
          reader.readAsDataURL(firstImage);
          filename.appendChild(img);
        }
        const list = document.createElement('ul');
        list.className = 'ui-fileupload__list';
        for (const f of files) {
          const li = document.createElement('li');
          li.textContent = f.name + ' · ' + fmtBytes(f.size);
          list.appendChild(li);
        }
        filename.appendChild(list);
      };
      input.addEventListener('change', render);
      // Initial render for SSR-restored states (some browsers
      // restore input.files on back-nav).
      render();

      const onEnter = (e) => {
        e.preventDefault();
        zone.closest('[data-fui-comp="ui-fileupload"]')?.classList.add('is-dragover');
      };
      const onLeave = (e) => {
        e.preventDefault();
        // dragleave fires when moving to a child — guard via relatedTarget.
        if (zone.contains(e.relatedTarget)) return;
        zone.closest('[data-fui-comp="ui-fileupload"]')?.classList.remove('is-dragover');
      };
      const onDrop = (e) => {
        e.preventDefault();
        zone.closest('[data-fui-comp="ui-fileupload"]')?.classList.remove('is-dragover');
        const files = e.dataTransfer && e.dataTransfer.files;
        if (!files || files.length === 0) return;
        if (input.disabled) return;
        // Assign via DataTransfer so the input's `files` becomes the
        // dropped list — input.files is read-only except through a
        // DataTransfer object.
        const dt = new DataTransfer();
        for (const f of files) {
          if (!input.multiple && dt.items.length > 0) break;
          dt.items.add(f);
        }
        input.files = dt.files;
        input.dispatchEvent(new Event('change', { bubbles: true }));
      };
      zone.addEventListener('dragenter', onEnter);
      zone.addEventListener('dragover', onEnter);
      zone.addEventListener('dragleave', onLeave);
      zone.addEventListener('drop', onDrop);
    }
  }

  // Module-level registration:
  //   1. Expose the scanner on the global namespace so core's MutationObserver
  //      + SPA-nav handler can re-run it after DOM swaps.
  //   2. Run it immediately on whatever's already in the DOM (the module
  //      loaded because a marker was detected — the module's first job is
  //      to wire that marker right now, not wait for the next event).
  window.__gofastr = window.__gofastr || {};
  window.__gofastr.scanFileUploads = wireFileUploads;
  // Back-compat alias for the previous monolithic runtime — kept so
  // any external caller of the legacy name keeps working through the
  // transition.
  window.__fuiWireFileUploads = wireFileUploads;
  // Per-module rescan registered with core. Core dispatches
  // `gofastr:navigate` after every SPA-nav swap and iterates
  // _moduleScanners — modules that need to wire new DOM (drop zones in
  // a freshly-swapped page) re-run here. wireFileUploads is idempotent
  // via __fuiWired flag, so re-running on the same node is a no-op.
  ((window.__gofastr._moduleScanners ||= {})).fileupload = wireFileUploads;
  // Mark the module as loaded so the loader's Promise resolves
  // synchronously on re-load.
  (window.__gofastr.loadedModules ||= {}).fileupload = true;
  wireFileUploads(document);
})();
