// FileDropzone runtime module — adds two augmentations on top of the
// existing data-fui-fileupload drag-drop hook:
//
//   1. Filename display: after change on a file input inside a
//      [data-fui-comp="ui-dropzone"], replace the .ui-dropzone__filename
//      text with the file count + first filename.
//
//   2. Image preview strip: when the input carries
//      [data-fui-dropzone-preview], FileReader-read each image file
//      and render <img> tags into the sibling
//      [data-fui-dropzone-preview-for="<input-id>"] container.
(function () {
  'use strict';

  function updateFilename(input) {
    const root = input.closest('[data-fui-comp="ui-dropzone"]');
    if (!root) return;
    const out = root.querySelector('.ui-dropzone__filename');
    if (!out) return;
    const files = input.files;
    if (!files || files.length === 0) {
      out.textContent = '';
      return;
    }
    if (files.length === 1) {
      out.textContent = files[0].name;
    } else {
      out.textContent = files.length + ' files — ' + files[0].name + ' …';
    }
  }

  function updatePreviews(input) {
    if (!input.hasAttribute('data-fui-dropzone-preview')) return;
    const container = document.querySelector(
      '[data-fui-dropzone-preview-for="' + input.id + '"]'
    );
    if (!container) return;
    container.innerHTML = '';
    const files = input.files;
    if (!files) return;
    Array.prototype.forEach.call(files, function (f) {
      if (!/^image\//.test(f.type)) return;
      const img = document.createElement('img');
      img.className = 'ui-dropzone__preview';
      img.alt = f.name;
      const reader = new FileReader();
      reader.onload = function (ev) {
        img.src = ev.target.result;
      };
      reader.readAsDataURL(f);
      container.appendChild(img);
    });
  }

  document.addEventListener('change', function (ev) {
    const t = ev.target;
    if (!t || t.tagName !== 'INPUT' || t.type !== 'file') return;
    const root = t.closest('[data-fui-comp="ui-dropzone"]');
    if (!root) return;
    updateFilename(t);
    updatePreviews(t);
  });
})();
