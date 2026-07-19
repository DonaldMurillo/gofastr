# Background compute with Web Workers and WebAssembly

GoFastr keeps the core browser runtime within a 12 KB gzip budget. CPU-heavy
work does not belong in that core or on the main thread: register a
self-contained Web Worker, optionally register a WebAssembly module, and let
the demand-loaded `compute` module dispatch structured-clone messages to it.
Workers and Wasm bytes are served from same-origin, content-addressed URLs, so
they remain compatible with the default CSP and immutable browser caching.

## Public API

| API | Purpose |
|---|---|
| `compute.RegisterWorker(name string, js []byte)` | Register a classic, self-contained Worker script. |
| `compute.RegisterWASM(name string, wasm []byte)` | Register WebAssembly bytes. |
| `window.__gofastr.compute.task(worker, fn, payload)` | Send one request and return a `Promise` for its result. |
| `window.__gofastr.compute.wasmURL(name)` | Return the registered module's versioned same-origin URL. |
| `window.__gofastr.compute.dispose(worker)` | Terminate the cached worker and reject its pending tasks. |
| `data-fui-compute` | Demand-load the client module. It is a trigger only. |

Names are 1–64 lowercase ASCII letters, digits, `-`, or `_`. Registration
copies the input bytes and computes a full SHA-256 hash. Re-registering the
same name and bytes is a no-op. Reusing the same name for different bytes of
the same kind panics at startup. A worker and a Wasm module may share a name.

The UI host automatically emits an inert `#gofastr-compute-assets` JSON
manifest and mounts these immutable routes:

```text
/__gofastr/compute/<name>.js?v=<sha256>
/__gofastr/compute/<name>.wasm?v=<sha256>
```

Worker responses use `application/javascript`; Wasm responses use
`application/wasm` so `WebAssembly.instantiateStreaming` can compile while
the response arrives.

## Register embedded assets

Application binaries normally embed both assets. Registration must happen
before the UI host serves its first page:

```go
package main

import (
	_ "embed"

	"github.com/DonaldMurillo/gofastr/core-ui/compute"
)

//go:embed web/sum-worker.js
var sumWorker []byte

//go:embed web/sum.wasm
var sumWASM []byte

func registerCompute() {
	compute.RegisterWorker("sum-worker", sumWorker)
	compute.RegisterWASM("sum", sumWASM)
}

func main() {
	registerCompute()
	// Construct framework.App, register screens, mount uihost, then Start.
}
```

The normal `fwApp.Mount(uihost.New(site))` wiring serves the compute routes;
there is no extra handler to mount. See [UI wiring](ui-wiring.md) for a
complete host `main.go`.

Add the load marker to the server-rendered screen. It declares no work and
needs no worker-name companion attribute:

```go
html.Div(html.DivConfig{
	ExtraAttrs: html.Attrs{"data-fui-compute": ""},
},
	html.Button(html.ButtonConfig{ID: "run-sum", Label: "Add 19 + 23"}),
	html.Span(html.TextConfig{ID: "sum-result"}),
)
```

Client code must be an external same-origin script. `WithExtraScripts` is the
usual way to include it. Awaiting `loadModule` also covers a click that lands
before the marker scanner's request finishes:

```js
// web/compute-client.js
const button = document.getElementById('run-sum');
const output = document.getElementById('sum-result');

button.addEventListener('click', async () => {
  button.disabled = true;
  button.setAttribute('aria-busy', 'true');
  try {
    await window.__gofastr.loadModule('compute');
    const compute = window.__gofastr.compute;
    const result = await compute.task('sum-worker', 'sum', {
      wasmURL: compute.wasmURL('sum'),
      a: 19,
      b: 23,
    });
    output.textContent = String(result);
  } catch (error) {
    output.textContent = error.message;
  } finally {
    button.disabled = false;
    button.removeAttribute('aria-busy');
  }
});
```

Serve that file from the app's static directory and include it with
`uihost.WithExtraScripts("/compute-client.js")`.

## Worker protocol

GoFastr deliberately ships no generated worker wrapper and no
`WorkerShimJS()`. A bare worker implements this small protocol directly,
keeping the demand module small and making the worker independently testable.

Request from the page:

```js
{ id, fn, payload }
```

Response from the worker:

```js
{ id, ok: true, result }
{ id, ok: false, error: "message" }
```

`id` must be copied unchanged. `payload` and `result` must be values supported
by the browser's structured-clone algorithm. An `ok: false` response rejects
the task Promise with `Error(error)`. An uncaught Worker error rejects every
pending task for that worker and discards the failed instance. A task with no
response rejects after 30 seconds; the timed-out worker is terminated so a
later task gets a fresh instance.

One Worker instance is reused per registered name. Use `dispose(name)` when an
island owns a worker whose memory should be released before the page ends.
GoFastr does not terminate workers on SPA navigation because the runtime and
page process remain alive across those navigations.

## Worked Wasm worker

This complete `web/sum-worker.js` caches one Wasm instantiation, handles
`fn: "sum"`, and returns protocol errors instead of leaving page Promises
pending:

```js
let exportsPromise;

function wasmExports(url) {
  if (!exportsPromise) {
    exportsPromise = WebAssembly.instantiateStreaming(fetch(url))
      .then(({ instance }) => instance.exports);
  }
  return exportsPromise;
}

self.onmessage = async ({ data: message }) => {
  const { id, fn, payload } = message;
  try {
    if (fn !== 'sum') throw new Error(`unknown function: ${fn}`);
    const wasm = await wasmExports(payload.wasmURL);
    const result = wasm.sum(payload.a, payload.b);
    self.postMessage({ id, ok: true, result });
  } catch (error) {
    self.postMessage({
      id,
      ok: false,
      error: error instanceof Error ? error.message : String(error),
    });
  }
};
```

The corresponding module must export a function named `sum` accepting two
WebAssembly `i32` values and returning one `i32`. The browser fetch is the
versioned URL supplied by `compute.wasmURL`; do not hard-code the route or
hash in the worker.

## Compiling Go or TinyGo

### Standard Go, browser target

A `js/wasm` program uses Go's JavaScript runtime glue:

```sh
GOOS=js GOARCH=wasm go build -o web/sum.wasm ./cmd/sumwasm
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/wasm_exec.js
```

The worker must load that same-origin glue with
`importScripts("/wasm_exec.js")`, create `new Go()`, instantiate with
`go.importObject`, and start `go.run(instance)`. Use the `wasm_exec.js` from
the same Go major/minor toolchain that produced the Wasm binary. A `js/wasm`
binary is not the bare-export module used in the worked example above; expose
operations through `syscall/js` or add a worker adapter around its Go runtime.

Go 1.24 and newer also support `//go:wasmexport` with a WASI reactor build:

```go
//go:wasmexport sum
func sum(a, b int32) int32 { return a + b }
```

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o web/sum.wasm ./cmd/sumwasm
```

A browser does not provide WASI imports by itself. Pass a compatible WASI
import object to `instantiateStreaming` or use a browser-targeted toolchain;
do not assume a `wasip1` reactor will instantiate with no imports.

### TinyGo

```sh
tinygo build -target wasm -o web/sum.wasm ./cmd/sumwasm
cp "$(tinygo env TINYGOROOT)/targets/wasm_exec.js" web/tinygo-wasm_exec.js
```

TinyGo's `wasm` target also has runtime-glue and export conventions. Use the
glue shipped by the exact TinyGo version that built the module and adapt the
worker to its exported functions. If the selected TinyGo target emits WASI,
the same WASI-import caveat applies. Confirm the final binary exports `sum`
with `wasm-tools print`, `wasm-objdump -x`, or the toolchain's equivalent
before wiring the page API.

## CSP, origins, and threads

Workers are always created from the registered same-origin URL. GoFastr does
not use `blob:` or `data:` workers, so the document's `default-src 'self'`
policy remains intact. Dedicated workers enforce the CSP delivered with their
own script response. The worker route therefore adds the narrow
`script-src 'self' 'wasm-unsafe-eval'` permission required by current browsers
for WebAssembly compilation. This permits Wasm compilation; it does **not**
enable JavaScript `eval` or `new Function`.

Cross-origin worker or Wasm URLs are not part of this API. Register the bytes
with the app instead of pointing a worker at a CDN.

`SharedArrayBuffer` and WebAssembly threads require cross-origin isolation,
which means both COOP and COEP. GoFastr sets COOP to `same-origin` but does not
enable COEP. Shared memory and Wasm threads are therefore unavailable here.
No COEP header is added by compute registration.

## Common mistakes

- **No `data-fui-compute` marker and no explicit `loadModule("compute")`:**
  `window.__gofastr.compute` has not been installed yet.
- **Returning without a response:** every request must eventually post an
  `ok: true` or `ok: false` response with the original `id`; otherwise it
  times out and the worker is terminated.
- **Hard-coding `/__gofastr/compute/sum.wasm`:** use `wasmURL("sum")` so the
  immutable cache key changes with the registered bytes.
- **Using module imports in the registered worker:** registered workers are
  classic, self-contained scripts. Bundle dependencies into the worker or
  use same-origin `importScripts` for required runtime glue.
- **Expecting threads:** no COEP means no cross-origin isolation and no
  `SharedArrayBuffer`.
