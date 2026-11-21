# Background compute (Web Workers & WebAssembly)

Keep CPU-heavy browser work off the main thread and out of GoFastr's tiny core runtime by registering classic Web Workers and optional WebAssembly modules in Go (`core-ui/compute`) and invoking them on demand via the `compute` runtime module.

## API summary

| API | Purpose |
| --- | --- |
| `compute.RegisterWorker(name, js)` | Register a classic, self-contained worker script |
| `compute.RegisterWASM(name, wasm)` | Register WebAssembly bytes |
| `window.__gofastr.compute.task(worker, fn, payload)` | Send a request and await its `Promise` result |
| `window.__gofastr.compute.wasmURL(name)` | Get the content-addressed `/__gofastr/compute/<name>.wasm?v=<sha256>` URL |
| `window.__gofastr.compute.dispose(worker)` | Terminate a cached worker and reject pending tasks |
| `data-cui-compute` | Trigger demand-loading of the `compute` client module |

## Registering embedded assets

Register workers and Wasm bytes before `uihost` serves its first page (`uihost.New` mounts `/__gofastr/compute/<name>.{js,wasm}?v=<sha256>` automatically with immutable caching):

```go
//go:embed web/sum-worker.js
var sumWorker []byte

//go:embed web/sum.wasm
var sumWASM []byte

func init() {
	compute.RegisterWorker("sum-worker", sumWorker)
	compute.RegisterWASM("sum", sumWASM)
}
```

Add the `data-cui-compute` marker on the screen:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core-ui/html"
-->
```go
html.Div(html.DivConfig{
	ExtraAttrs: html.Attrs{"data-cui-compute": ""},
},
	html.Button(html.ButtonConfig{ID: "run-sum", Label: "Add 19 + 23"}),
	html.Span(html.TextConfig{ID: "sum-result"}),
)
```

Invoke from an external classic script (`uihost.WithExtraScripts("/compute-client.js")` — wrap `await` inside an `async` handler since classic scripts do not allow top-level `await`):

```js
document.getElementById('run-sum')?.addEventListener('click', async () => {
  await window.__gofastr.loadModule('compute');
  const result = await window.__gofastr.compute.task('sum-worker', 'sum', {
    wasmURL: window.__gofastr.compute.wasmURL('sum'),
    a: 19,
    b: 23,
  });
  document.getElementById('sum-result').textContent = String(result);
});
```

## Worker message protocol

Every worker implements a tiny structured-clone request/response contract (no generated shim):
- **Incoming:** `{ id, fn, payload }`
- **Reply:** `{ id, ok: true, result }` or `{ id, ok: false, error: "message" }`

```js
// web/sum-worker.js
self.onmessage = ({ data: { id, fn, payload } }) => {
  if (fn === 'sum') self.postMessage({ id, ok: true, result: payload.a + payload.b });
  else self.postMessage({ id, ok: false, error: 'unknown fn: ' + fn });
};
```

Workers reuse one instance per name across SPA navigations, time out silent tasks after 30 seconds (terminating the hung worker), and reject pending tasks on uncaught errors.

## Compiling Go or TinyGo to Wasm

- **Standard Go (`GOOS=js GOARCH=wasm`):** pair with the matching `wasm_exec.js` from `$(go env GOROOT)/lib/wasm/wasm_exec.js` loaded via same-origin `importScripts`.
- **Go 1.24+ WASI reactor (`//go:wasmexport`, `GOOS=wasip1 GOARCH=wasm -buildmode=c-shared`):** supply a WASI import object in the worker (browsers do not provide WASI imports natively).
- **TinyGo (`tinygo build -target wasm`):** pair with TinyGo's matching `wasm_exec.js`.

## CSP and threading limits

- Workers load from same-origin URLs (no `blob:`/`data:` workers), preserving `default-src 'self'`. The worker script route sets `script-src 'self' 'wasm-unsafe-eval'` to allow Wasm compilation without enabling JS `eval`.
- GoFastr does **not** enable COEP, so `SharedArrayBuffer` and Wasm threads are not available here.

## Common mistakes

- **Omitting `data-cui-compute` and skipping `loadModule("compute")`:** `window.__gofastr.compute` will be undefined.
- **Dropping a reply in the worker:** every incoming message must reply with its `id` or it times out after 30s and kills the worker.
- **Hardcoding `/__gofastr/compute/sum.wasm`:** always call `compute.wasmURL("sum")` so cache-busting hashes update on rebuild.
- **Using ES module `import` syntax inside a worker:** registered workers are classic scripts; bundle dependencies or use same-origin `importScripts`.
- **Expecting `SharedArrayBuffer` / Wasm threads:** COEP is intentionally off.
