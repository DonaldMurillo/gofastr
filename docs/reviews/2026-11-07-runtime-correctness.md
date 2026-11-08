# Runtime correctness review — November 7, 2026

Reviewed revision: `953ccc08256e825bdc17254c463a5695a27092b6`.

## C09 — P3: string action messages disappear in the toast fallback

`framework/uihost/uihost.go:3853` returns a string `message` from an action.
`core-ui/runtime/frag/kernel.js:494` sends that string to `_toastOrFallback`.
The normal feedback module accepts strings and converts them to title objects
(`framework/headless/feedback.js:283`), but `_fallbackToast` immediately rejects
the string because it lacks `.title` (`kernel.js:756`). The fallback therefore
drops the message when the module cannot load or has no stack to render into.
The kernel's documented purpose is to retain a visible message despite module
load failure. Object-config toasts still render in both fallback conditions.

## C10 — P2: preserved global signals are not reflected into swapped nodes

Root: `core-ui/runtime/frag/nav.js:953`, not `signals.js` as named in the
reviewer's report. `mergeSeedFromDOM` preserves an existing global store value
but never renders it into newly inserted bound nodes. `swapAtSlot` calls that
merge after replacing the markup (`nav.js:491`). A cart count changed to 1
survives in the store while the destination badge displays its server seed 0.
The same mismatch can occur when replaying cached markup on Back. This violates
the signal binding/store relationship (`signals.js:34`) while retaining the
global-store survival promised by `core-ui/ARCHITECTURE.md:1401`.

### Evidence boundaries

The reviewer reports fresh-page and Back-navigation observations in real Chrome
using the composed runtime, with store 1 and badge 0 after both swaps. Those
browser runs are reviewer evidence, not independently executed coordinator runs.
The coordinator's separate self-contained Node probe below evaluates the actual
production method bodies, with a small DOM adapter. It checks the action response
dispatch and both toast fallback arms, then the seed-merge/rendering seam with
an already-mutated global. It does not simulate a whole browser or independently
claim a complete navigation e2e run. Changed page-signal and explicit re-render
controls distinguish DOM adapter failures from missing global re-rendering.

One root is counted per defect; fresh and cached navigation are not separate bugs.
No production fix is included.

### Retained independent observation probe

Extract this JavaScript block to `/tmp/gofastr-runtime-observation.cjs`, then
run `node /tmp/gofastr-runtime-observation.cjs "$PWD"` from the checkout root.
It needs Node only, with no dependency install. Assertions describe current
defects and controls, not future regression expectations after fixes.

```javascript
'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const checkout = process.argv[2] || process.cwd();
const read = (name) => fs.readFileSync(path.join(checkout, 'core-ui/runtime/frag', name + '.js'), 'utf8');
const kernel = read('kernel');
const signals = read('signals');
const navigation = read('nav');
function section(source, start, end) {
    const first = source.indexOf(start);
    const last = source.indexOf(end, first);
    assert(first >= 0 && last > first, 'production source anchors changed');
    return source.slice(first, last);
}
class Element {
    constructor(attributes = {}, text = '') {
        this.attributes = attributes;
        this.textContent = text;
        this.children = [];
        this.style = {};
    }
    setAttribute(name, value) { this.attributes[name] = String(value); }
    getAttribute(name) { return this.attributes[name] ?? null; }
    hasAttribute(name) { return Object.hasOwn(this.attributes, name); }
    appendChild(child) { this.children.push(child); return child; }
    addEventListener() {}
    remove() { this.removed = true; }
}
let bindings = [];
const notices = [];
const context = {
    console, URL, CSS: { escape: (value) => value },
    location: { pathname: '/orders' },
    window: { __gofastr: {} },
    document: {
        createElement: () => new Element(),
        querySelectorAll: (selector) => {
            const match = /^\[data-cui-signal="([^"]+)"\]$/.exec(selector);
            return match ? bindings.filter((node) => node.getAttribute('data-cui-signal') === match[1]) : [];
        },
    },
    own: (object, name) => Object.hasOwn(object, name),
    isReservedSignalKey: (name) => ['__proto__', 'constructor', 'prototype'].includes(name),
    _json: JSON.parse,
    say: (_key, fallback) => fallback,
    _lp: (value) => value,
    _unlocalize: (value) => value,
    _fetch: async () => ({ ok: true, redirected: false, json: async () => ({ status: 'ok', message: 'Action processed' }) }),
    doc: { singleton: (_name, make) => { const node = make(); notices.push(node); return node; } },
};
vm.createContext(context);
const actionSource = section(kernel, '    async _serverActionFor(', '\n    /** loadCSS');
const toastSource = section(kernel, '    _toastOrFallback(cfg)', '\n  };');
vm.runInContext('Object.assign(window.__gofastr, ({' + actionSource + toastSource + '}));', context);
vm.runInContext(section(signals, '  Object.assign(window.__gofastr, {', '\n  // Client signal mutations'), context);
vm.runInContext(section(navigation, '  const mergeSeedFromDOM =', '\n  // Link clicks:'), context);
const runtime = context.window.__gofastr;
function renderedText(node) {
    return node.textContent + node.children.map(renderedText).join('');
}
async function observe() {
    for (const mode of ['module_unavailable', 'missing_stack']) {
        runtime.loadModule = mode === 'module_unavailable'
            ? () => Promise.reject(new Error('module unavailable')) : () => Promise.resolve();
        runtime.toast = () => null;
        const beforeString = notices.length;
        await runtime._toastOrFallback('Action processed');
        assert.equal(notices.length, beforeString);
        await runtime._toastOrFallback({ title: 'Object control' });
        assert.equal(notices.length, beforeString + 1);
        assert.match(renderedText(notices.at(-1)), /Object control/);
        console.log(mode + ': string message invisible; object fallback rendered');
    }
    runtime.loadModule = () => Promise.reject(new Error('module unavailable'));
    const beforeAction = notices.length;
    const answer = await runtime._serverActionFor('save-component', 'save');
    await Promise.resolve();
    await Promise.resolve();
    assert.equal(answer.message, 'Action processed');
    assert.equal(notices.length, beforeAction);
    console.log('action dispatch: successful response message returned, no fallback notice');
    const originalBadge = new Element({ 'data-cui-signal': 'cart' }, '0');
    bindings = [originalBadge];
    runtime.setSignal('cart', 0);
    runtime.setSignal('cart', 1);
    assert.equal(originalBadge.textContent, '1');
    const destinationBadge = new Element({ 'data-cui-signal': 'cart' }, '0');
    const pageBadge = new Element({ 'data-cui-signal': 'quantity' }, '0');
    bindings = [destinationBadge, pageBadge];
    const island = new Element({}, JSON.stringify({ p: { quantity: 10 }, g: { cart: 0 } }));
    runtime._mergeSeed({ querySelector: () => island });
    assert.equal(island.removed, true);
    assert.equal(runtime.getSignal('cart'), 1);
    assert.equal(destinationBadge.textContent, '0');
    assert.equal(pageBadge.textContent, '10');
    console.log('partial merge: global store=1, destination badge=0; changed page badge=10');
    runtime.setSignal('cart', runtime.getSignal('cart'));
    assert.equal(destinationBadge.textContent, '1');
    console.log('explicit re-render control: destination badge=1');
}
observe().catch((error) => { console.error(error); process.exitCode = 1; });
```

### Independently executed results

- Both module-unavailable and no-stack arms drop a string message; each renders
  the object-config control. The actual action method returns the string
  message but produces no fallback notice.
- After replacing the bound node and merging the partial seed, the global
  store is 1 and badge is 0; a changed page signal correctly renders 10.
- Explicitly rendering the preserved global value updates the new badge to 1.
- These Node results are narrower than the reviewer's browser navigation runs.

## Not counted: string "0" boolean attribute interpretation

The reviewer's quoted comment enumerates numeric `0`, not string `"0"`.
All enumerated values match the observed renderer. Toggle coercion treating
`"0"` differently is surprising, but no established contract requires the two
operations to use identical coercion. This candidate remains uncounted.

## Broader-suite validation limitation

The coordinator ran:

```sh
go test -p 2 -short -count=1 ./core-ui/runtime/ ./core/moduleproto/ ./framework/pluginhost/
```

`core-ui/runtime` failed at the default ten-minute package timeout
(`600.392s`). `TestNestedTargetsFullLoads` had been running for one second
when the package-wide alarm fired; this is not evidence that that individual
test deadlocked. The output also contained CDP `EventTopLayerElementsUpdated`
warnings, whose causal relationship to duration was not established.
`core/moduleproto` and `framework/pluginhost` passed (9.349s and 0.409s).

`-short` does not exclude the runtime's browser tests. The broad runtime
suite is therefore not claimed green, and its duration alone is not a new
functional finding. Use named browser-free tests for quick composition checks
and an explicitly budgeted, JSON-logged run for full browser validation.

The coordinator's subsequent named checks `TestComposedRuntimeIsSymbolComplete`
and `TestKernelMarkupMatchesGo` pass (0.371s). These two source/composition tests
are not a substitute for the full browser suite.
