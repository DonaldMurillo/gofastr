# Reactive client correctness review — November 7, 2026

Independently reproduced at `953ccc08256e825bdc17254c463a5695a27092b6`.
Two accepted P3 root causes; no production fixes are included.

## C13 — Clean server 5xx close exposes the wrong WebSocket reason class

Root: `core-ui/runtime/src/ws.js:154`–`:155`. The 4400–4499 band has an
explicit refusal branch, but 4500–4599 falls through to `wasClean` alone.
A clean close in that band therefore exposes `closed` rather than `error`.

The public API contract at `ws.js:79` and the user guide at
`framework/docs/content/reactivity.md:255` expressly classify 4000 + 5xx
server closes as `error`, retried. The result is available both through
`onGenerationEnd({reasonClass})` and the live handle's `status.reasonClass`.

The coordinator loaded the actual, unmodified ws.js into a fresh Node VM
for every case, invoked its public connectWebSocket API, opened the socket,
and delivered a close event. Results:

| Code / wasClean | Observed class | Contract class | Retry control |
| --- | --- | --- | --- |
| 4503 / true | closed | error | yes |
| 4599 / true | closed | error | yes |
| 4503 / false | error | error | yes |
| 4409 / true | refused | refused | no; status.refused = 409 |
| 1000 / true | closed | closed | yes |
| 1006 / false | error | error | yes |

Retry behavior is correct in every case. This is the exposed reason class
being wrong, not failed reconnect, lost transport messages or a claim of
media recovery. All cases run sequentially in isolated contexts. The socket
and timers are deterministic adapters: no real server/browser close handshake
was executed. The close-event classification is measured directly; the
reviewer's browser-event provenance discussion remains an inference.

## C14 — Identical structured poll values rewrite bindings every tick

Root: `core-ui/runtime/src/poll.js:269`. Widget polling compares decoded
values by reference identity. JSON-decoded arrays/objects are fresh references
every response, so an unchanged structured signal always reaches setSignal.

The public Builder.Poll contract at `core-ui/widget/widget.go:434`–`:438`
promises to skip the DOM write when the value is unchanged. The user guide
at `framework/docs/content/widgets.md:234` describes reapplying only changed
signals. SignalFunc returns any supported JSON value, not just scalars.
`core-ui/runtime/src/widgets.js:441`–`:446` explicitly links skipping no-op
writes to avoiding flashes/re-layout.

The coordinator executes actual poll.js and the actual Object.assign signal
namespace from `core-ui/runtime/frag/signals.js`, including setSignal and its
after-update hooks. Unlike the reviewer's harness, the coordinator does not
reimplement the signal setter/flash logic. The adapter supplies fresh JSON
parses of an identical response, deterministic timers, and two text bindings.
One structured binding and one scalar binding receive initial hydration.

- Six byte-identical JSON polls: structured binding has seven text writes,
  seven flash-class additions and seven scroll-bottom writes (including
  hydration). Scalar control has one of each: hydration only.
- A seventh poll changing the structured object's done field renders its
  new text. The unchanged scalar remains skipped.
- Values stay correct throughout. The defect is redundant visible-update
  hook execution and DOM work, not stale or corrupted data.

The counter assertions measure the production hooks' calls against a minimal
DOM adapter. Actual browser animation, reflow cost and scroll position were
not measured. The coordinator used synthetic valid /state response bytes,
not a live widget server; the reviewer separately retained a real Go widget
/state producer. This root is independent of C10's navigation seed merge.

## Retained independent observation probe

Validation: the Node observation probe and controls pass. Fresh named
browser-free runtime composition tests and widget Poll catalog/terminal-state
tests pass. The earlier broad runtime browser-suite timeout remains a
separate limitation; this report does not claim that suite passed.

Run this script with the checkout path as its first argument. It asserts the
currently observed discrepancies and passing controls, not desired post-fix
behavior. Source and result log currently live under
`/tmp/gofastr-coordinator-reactive/`.

```sh
node /tmp/gofastr-coordinator-reactive/observation.cjs "$PWD"
```

```javascript
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = process.argv[2] || process.cwd();
const source = relative => fs.readFileSync(path.join(root, relative), 'utf8');

function timers() {
  let nextId = 0;
  const pending = new Map();
  return {
    pending,
    setTimeout(callback, delay) { const id = ++nextId; pending.set(id, { callback, delay }); return id; },
    clearTimeout(id) { pending.delete(id); },
  };
}

function checkWebSocket() {
  for (const sample of [
    { code: 4503, clean: true, observed: 'closed', expected: 'error', retry: true },
    { code: 4599, clean: true, observed: 'closed', expected: 'error', retry: true },
    { code: 4503, clean: false, observed: 'error', expected: 'error', retry: true },
    { code: 4409, clean: true, observed: 'refused', expected: 'refused', retry: false },
    { code: 1000, clean: true, observed: 'closed', expected: 'closed', retry: true },
    { code: 1006, clean: false, observed: 'error', expected: 'error', retry: true },
  ]) {
    const clock = timers();
    const sockets = [];
    class Socket {
      constructor(url) { this.url = url; this.readyState = 1; sockets.push(this); }
      close() { this.readyState = 3; }
    }
    const context = vm.createContext({ window: {}, WebSocket: Socket, setTimeout: clock.setTimeout, clearTimeout: clock.clearTimeout });
    vm.runInContext(source('core-ui/runtime/src/ws.js'), context);
    const ended = [];
    const handle = context.window.__gofastr.connectWebSocket('ws://fixture.invalid/state', {
      onGenerationEnd(info) { ended.push(info.reasonClass); },
    });
    sockets[0].onopen();
    sockets[0].onclose({ code: sample.code, wasClean: sample.clean });
    assert.deepEqual(ended, [sample.observed]);
    assert.equal(handle.status.reasonClass, sample.observed);
    assert.equal(handle.status.refused, sample.code === 4409 ? 409 : 0);
    const retries = [...clock.pending.values()].filter(timer => timer.delay === 1000);
    assert.equal(retries.length, sample.retry ? 1 : 0);
    if (sample.retry) { retries[0].callback(); assert.equal(sockets.length, 2); }
    handle.close();
    console.log(`WS ${sample.code} clean=${sample.clean}: observed=${sample.observed}, contract=${sample.expected}, retry=${sample.retry}`);
  }
}

function binding(name) {
  let text = '';
  return {
    name, writes: 0, flashes: 0, scrolls: 0,
    get textContent() { return text; },
    set textContent(value) { text = value; this.writes++; },
    getAttribute(key) { return key === 'data-cui-signal' ? name : null; },
    hasAttribute(key) { return key === 'data-cui-flash-on-update' || key === 'data-cui-scroll-bottom-on-update'; },
    querySelector() { return null; },
    get offsetWidth() { return 20; },
    get scrollHeight() { return 100; },
    set scrollTop(value) { assert.equal(value, 100); this.scrolls++; },
  };
}

async function checkPolling() {
  const structured = binding('jobs.progress');
  const scalar = binding('jobs.status');
  for (const node of [structured, scalar]) {
    node.classList = { remove() {}, add() { node.flashes++; } };
  }
  const document = {
    hidden: false,
    addEventListener() {}, removeEventListener() {},
    querySelectorAll(selector) {
      if (selector === '[data-cui-poll]') return [];
      return [structured, scalar].filter(node => selector === `[data-cui-signal="${node.name}"]`);
    },
  };
  const clock = timers();
  const state = { 'jobs.progress': { done: 2, total: 4 }, 'jobs.status': 'working' };
  let bytes = JSON.stringify(state);
  let requests = 0;
  const context = vm.createContext({
    window: { __gofastr: {}, matchMedia() { return { matches: false }; } },
    document, CSS: { escape(value) { return value; } }, console,
    setTimeout: clock.setTimeout, clearTimeout: clock.clearTimeout,
    fetch: async () => {
      requests++;
      const snapshot = bytes;
      return { ok: true, headers: { get() { return null; } }, json: async () => JSON.parse(snapshot) };
    },
  });
  vm.runInContext('const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);', context);
  const signals = source('core-ui/runtime/frag/signals.js');
  const boundary = signals.indexOf('  // Client signal mutations are core behavior.');
  assert.notEqual(boundary, -1);
  vm.runInContext(signals.slice(0, boundary), context);
  const runtime = context.window.__gofastr;
  runtime.fetch = context.fetch;
  runtime._originOK = () => true;
  for (const [name, value] of Object.entries(JSON.parse(bytes))) runtime.setSignal(name, value);
  await Promise.resolve();
  vm.runInContext(source('core-ui/runtime/src/poll.js'), context);
  const entry = {};
  runtime._widgetPoll({ name: 'jobs', statePath: '/widgets/jobs/state', pollMs: 5000 }, entry);
  for (let tick = 0; tick < 6; tick++) {
    entry.pollNow();
    for (let step = 0; step < 12; step++) await Promise.resolve();
  }
  assert.equal(requests, 6);
  assert.equal(runtime.pollStatus.ticks, 6);
  assert.equal(structured.writes, 7);
  assert.equal(structured.flashes, 7);
  assert.equal(structured.scrolls, 7);
  assert.equal(scalar.writes, 1);
  assert.equal(scalar.flashes, 1);
  assert.equal(scalar.scrolls, 1);
  console.log(`POLL identical JSON x6: structured writes/flash/scroll=${structured.writes}/${structured.flashes}/${structured.scrolls}; scalar=${scalar.writes}/${scalar.flashes}/${scalar.scrolls} (includes hydration)`);
  state['jobs.progress'].done = 3;
  bytes = JSON.stringify(state);
  entry.pollNow();
  for (let step = 0; step < 12; step++) await Promise.resolve();
  assert.equal(structured.writes, 8);
  assert.equal(scalar.writes, 1);
  assert.equal(structured.textContent, '{"done":3,"total":4}');
  entry.pollStop();
  console.log('POLL changed-object control: fresh value rendered; unchanged scalar still skipped');
}

(async () => { checkWebSocket(); await checkPolling(); })().catch(error => { console.error(error); process.exitCode = 1; });

```
