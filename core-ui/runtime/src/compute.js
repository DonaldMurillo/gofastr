// GoFastr runtime module — Web Worker + WebAssembly compute assets.
(() => {
  'use strict';

  const NS = window.__gofastr = window.__gofastr || {};
  const manifest = (() => {
    try {
      const el = document.getElementById('gofastr-compute-assets');
      return el ? JSON.parse(el.textContent || '{}') : {};
    } catch (_) {
      return {};
    }
  })();
  const workers = Object.create(null);
  let nextID = 0;
  const TIMEOUT_MS = 30000;

  const entryFor = (name, kind) => {
    if (typeof name !== 'string' ||
        !Object.prototype.hasOwnProperty.call(manifest, name) ||
        !manifest[name] || !manifest[name][kind]) {
      throw new Error('unknown compute ' + (kind === 'js' ? 'worker' : 'WASM asset') + ': ' + name);
    }
    return manifest[name];
  };

  const assetURL = (name, extension, hash) =>
    '/__gofastr/compute/' + encodeURIComponent(name) + '.' + extension + '?v=' + encodeURIComponent(hash);

  const failWorker = (name, error) => {
    const record = workers[name];
    if (!record) return;
    delete workers[name];
    record.worker.terminate();
    record.pending.forEach((pending) => {
      clearTimeout(pending.timer);
      pending.reject(error);
    });
    record.pending.clear();
  };

  const workerFor = (name) => {
    if (workers[name]) return workers[name];
    const entry = entryFor(name, 'js');
    const worker = new Worker(assetURL(name, 'js', entry.js));
    const record = { worker: worker, pending: new Map() };
    workers[name] = record;

    worker.onmessage = (event) => {
      const message = event.data;
      if (!message || !record.pending.has(message.id)) return;
      const pending = record.pending.get(message.id);
      record.pending.delete(message.id);
      clearTimeout(pending.timer);
      if (message.ok) {
        pending.resolve(message.result);
      } else {
        pending.reject(new Error(typeof message.error === 'string' ? message.error : 'compute task failed'));
      }
    };
    const onWorkerError = (event) => {
      if (event && event.preventDefault) event.preventDefault();
      const detail = event && event.message ? ': ' + event.message : '';
      failWorker(name, new Error('compute worker failed: ' + name + detail));
    };
    worker.onerror = onWorkerError;
    worker.onmessageerror = onWorkerError;
    return record;
  };

  NS.compute = {
    task(workerName, fn, payload) {
      return new Promise((resolve, reject) => {
        let record;
        try {
          record = workerFor(workerName);
        } catch (error) {
          reject(error);
          return;
        }
        const id = ++nextID;
        const pending = { resolve: resolve, reject: reject, timer: 0 };
        record.pending.set(id, pending);
        pending.timer = setTimeout(() => {
          failWorker(workerName, new Error('compute task timed out: ' + workerName));
        }, TIMEOUT_MS);
        try {
          record.worker.postMessage({ id: id, fn: fn, payload: payload });
        } catch (error) {
          record.pending.delete(id);
          clearTimeout(pending.timer);
          reject(error);
        }
      });
    },

    wasmURL(name) {
      const entry = entryFor(name, 'wasm');
      return assetURL(name, 'wasm', entry.wasm);
    },

    dispose(workerName) {
      failWorker(workerName, new Error('compute worker disposed: ' + workerName));
    },
  };

  (NS.loadedModules = NS.loadedModules || {}).compute = true;
})();
