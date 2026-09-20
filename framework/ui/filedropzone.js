// The FileDropzone preview strip: the one piece of the drop surface
// the headless layer does not have. FileReader-reads each chosen
// image and renders thumbnails into the
// [data-fui-dropzone-preview-for] container beside the dropzone.
//
// This module owns NOTHING but the thumbnails. The drag-and-drop
// forwarding, the chosen-files list and the pick announcement belong
// to the headless behaviour module on the data-hui-drop hooks, and
// this file deliberately duplicates none of them: turning image files
// into preview <img> elements is styling surface (a concern of the
// dressed layer), while the drop, the list and the sentence are
// behaviour (the headless module's). A filename is
// attacker-controlled — the uploader controls the name of the file
// they pick — so the alt lands through the attribute sink and the
// image through a data: URL, never an HTML sink.
(function () {
  'use strict';
  const NAME = 'filedropzone';
  const NS = window.__gofastr = window.__gofastr || {};
  // The kernel fetches a module once per page; anything that
  // evaluates this file a second time binds nothing twice.
  if (NS.loadedModules && Object.prototype.hasOwnProperty.call(NS.loadedModules, NAME)) return;
  NS.loadedModules = NS.loadedModules || {};
  NS.loadedModules[NAME] = true;

  function renderPreviews(input) {
    const container = document.querySelector(
      '[data-fui-dropzone-preview-for="' + CSS.escape(input.id) + '"]'
    );
    if (!container) return;
    container.textContent = '';
    const files = input.files || [];
    for (let i = 0; i < files.length; i++) {
      const f = files[i];
      if (!f.type || !f.type.startsWith('image/')) continue;
      const img = document.createElement('img');
      img.className = 'fui-drop__preview';
      img.alt = f.name;
      const reader = new FileReader();
      reader.onload = function (ev) { img.src = ev.target.result; };
      reader.readAsDataURL(f);
      container.appendChild(img);
    }
  }

  // Delegated from the document and bound once at load: markup that
  // arrives later (an island swap, a client navigation) needs no
  // re-binding, because the listener was never on the element.
  document.addEventListener('change', function (e) {
    const t = e.target;
    if (t && t.tagName === 'INPUT' && t.type === 'file' && t.hasAttribute('data-fui-dropzone-preview')) {
      renderPreviews(t);
    }
  });

  // The arrival pass: an input that already carries files (a
  // programmatic set that raced the module's load) gets its previews
  // rendered rather than waiting for a change that already happened.
  function scan(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const inputs = scope.querySelectorAll('input[data-fui-dropzone-preview]');
    for (let i = 0; i < inputs.length; i++) {
      if (inputs[i].files && inputs[i].files.length) renderPreviews(inputs[i]);
    }
  }
  scan(document);
  NS._moduleScanners = NS._moduleScanners || {};
  NS._moduleScanners[NAME] = scan;
})();
