// boot.js — kernel boot tail (always composed LAST, after every other fragment).
// These declarations run AFTER nav/signals/widgets-boot have loaded because
// _initialPass() is invoked synchronously here and calls updateActiveLink (nav)
// and _injectSignalAria (signals). Function declarations (loadModule,
// _scanForModules, _prefetch, _installEagerWidgetDelegators) are hoisted into
// the shared IIFE scope so earlier fragments can reference them at event time.

  // Event delegation: [data-action]
  document.addEventListener('click', (e) => {
    const target = e.target.closest('[data-action]');
    if (!target) return;

    const action = target.getAttribute('data-action');
    const componentId = closestAttr(e.target, 'data-component')
      ?? closestAttr(e.target, 'data-widget');

    if (componentId && action) {
      e.preventDefault();
      window.__gofastr.trigger(componentId, action, collectParams(target));
    }
  });

  // Event delegation: [data-action-type]
  for (const eventType of ['input', 'change', 'submit']) {
    document.addEventListener(eventType, (e) => {
      const target = e.target.closest(`[data-action-type="${eventType}"], [data-action-${eventType}]`);
      if (!target) return;

      const action = target.getAttribute(`data-action-${eventType}`) || target.getAttribute('data-action');
      if (!action) return;

      const componentId = closestAttr(e.target, 'data-component')
        ?? closestAttr(e.target, 'data-widget');

      if (componentId) {
        e.preventDefault();
        const params = { ...collectParams(target), value: e.target.value ?? '', eventType };
        window.__gofastr.trigger(componentId, action, params);
      }
    });
  }

  // Two-way binding: [data-bind]
  document.addEventListener('input', (e) => {
    const target = e.target.closest('[data-bind]');
    if (!target) return;
    const key = target.getAttribute('data-bind');
    if (!key) return;
    window.__gofastr.setState(key, target.value);
  });

  // Hydration on first interaction
  const hydrated = new Set();

  const hydrate = (componentId) => {
    if (hydrated.has(componentId)) return;
    hydrated.add(componentId);

    const el = document.querySelector(`[data-widget="${componentId}"]`)
      ?? document.querySelector(`[data-component="${componentId}"]`);
    if (!el) return;

    // data-behavior is the most privileged attribute the runtime reads:
    // it becomes a <script src>. Only the one shape the framework emits
    // is honoured — /__gofastr/widget/<id>.js, written by
    // core-ui/component/component.go — because any other value turns
    // attribute injection into script execution. CSP is not the answer
    // here: a SAME-origin JS route (an upload store, a generated SDK
    // .js, a plugin asset) executes under `default-src 'self'`.
    const scriptSrc = el.getAttribute('data-behavior');
    if (scriptSrc && /^\/__gofastr\/widget\/[A-Za-z0-9_-]+\.js(\?[^"'<>]*)?$/.test(scriptSrc)) {
      const script = document.createElement('script');
      script.src = scriptSrc;
      document.head.appendChild(script);
    } else if (scriptSrc) {
      console.warn('[gofastr] refused data-behavior src:', scriptSrc);
    }
  };

  // MutationObserver for auto-hydration
  const setupMutationObserver = () => {
    if (typeof MutationObserver === 'undefined') return;
    if (!document.body) return;

    const setupHydration = (el) => {
      const id = el.getAttribute('data-component') ?? el.getAttribute('data-widget');
      if (!id) return;
      el.addEventListener('focus', () => hydrate(id), { once: true });
      el.addEventListener('mouseenter', () => hydrate(id), { once: true });
    };

    const observeNode = (node) => {
      if (node.nodeType !== 1) return;
      if (node.getAttribute?.('data-component') || node.getAttribute?.('data-widget')) {
        setupHydration(node);
      }
      for (const child of node.querySelectorAll?.('[data-component], [data-widget]') ?? []) {
        setupHydration(child);
      }
      // Demand-load split runtime modules whose marker attributes show
      // up in injected subtrees (RPC innerHTML replacement, signal
      // swaps, island updates). Without this, dynamically-inserted
      // fileupload zones / popover triggers / toast stacks would never
      // load their module and behave as dead DOM.
      _scanForModules(node);
      // And re-run scanners of modules that ARE loaded so they wire
      // any newly-inserted elements (toast TTL, fileupload drop zones).
      const G = window.__gofastr;
      if (G && G._moduleScanners) {
        for (const name in G._moduleScanners) {
          if (G.loadedModules && G.loadedModules[name]) {
            try { G._moduleScanners[name](node); } catch (_) {}
          }
        }
      }
    };

    new MutationObserver((mutations) => {
      for (const m of mutations) {
        for (const node of m.addedNodes) observeNode(node);
      }
    }).observe(document.body, { childList: true, subtree: true });
  };

  if (document.body) {
    setupMutationObserver();
  } else {
    document.addEventListener('DOMContentLoaded', setupMutationObserver);
  }

  // SSE Island Support ships in core-ui/runtime/src/sse.js, loaded on
  // demand when <meta name="gofastr-sse"> is present on the page.
  // The module self-installs an EventSource and reflects "island"
  // events into matching [data-island] regions. Reconnect lives in
  // the module too.

  // FileUpload runtime has moved to its own demand-loaded module at
  // /__gofastr/runtime/fileupload.js. Core ships the loader + the
  // page-scan trigger below; the actual drag/drop wiring + filename
  // preview ships only when the page contains a [data-fui-fileupload]
  // zone (or when a `data-fui-prefetch="fileupload"` trigger is
  // hovered, whichever comes first).
  //
  // The legacy `window.__fuiWireFileUploads` is preserved by the
  // module itself for back-compat with external callers.

  // === MODULE LOADER ===================================================
  // loadModule(name) returns a cached Promise that resolves once the
  // named split-runtime module is loaded. Multiple callers for the
  // same name share one fetch. Modules self-register by setting
  // window.__gofastr.loadedModules[name] = true; the loader polls that
  // flag while the <script> downloads.
  //
  // Cache-busting: the host SSRs the per-module hash into a JSON
  // manifest under <script id="gofastr-runtime-modules">. The loader
  // reads it once; if a name is missing from the manifest, we fall
  // back to an un-versioned URL (works in dev, may pollute caches in
  // prod — the manifest is the source of truth).
  const _moduleManifest = (() => {
    try {
      const el = document.getElementById('gofastr-runtime-modules');
      if (!el) return {};
      return JSON.parse(el.textContent || '{}');
    } catch (_) { return {}; }
  })();
  const _modulePromises = {};
  function loadModule(name) {
    if (window.__gofastr.loadedModules?.[name]) {
      return Promise.resolve();
    }
    if (_modulePromises[name]) return _modulePromises[name];
    _modulePromises[name] = new Promise((resolve, reject) => {
      const v = _moduleManifest[name] || '';
      const url = '/__gofastr/runtime/' + name + '.js' + (v ? '?v=' + v : '');
      const s = document.createElement('script');
      s.src = url;
      s.async = false;
      s.onload = () => resolve();
      s.onerror = () => {
        // Drop the cached promise so a retry fires a fresh request.
        delete _modulePromises[name];
        reject(new Error('module failed'));
      };
      document.head.appendChild(s);
    });
    return _modulePromises[name];
  }

  // Live compositions keep one document-level bridge so a click or submit
  // that lands while rpc.js is downloading is retained. rpc-stub installs its
  // own static-only guard before boot runs, so static exports never install
  // this bridge or request the server-backed module.
  // The bridge calls preventDefault() BEFORE awaiting src/rpc.js, so a
  // module that never arrives (404 because the host does not serve
  // /__gofastr/runtime/<name>.js, or a network blip) would otherwise eat
  // the user's click or submit in silence. When RPC was compiled into
  // core that could not happen; carving it out reintroduced the risk, so
  // failure has to be loud, and a form has to still go somewhere.
  const _rpcUnavailable = () => {
    console.warn('[gofastr] RPC module unavailable — serve /__gofastr/runtime/rpc.js');
  };
  const _rpcFormFallback = (form) => {
    _rpcUnavailable();
    // Native submit is correct ONLY for a form the browser could have
    // submitted itself — a data-fui-spa form with an ordinary enctype.
    //
    // A data-fui-rpc form targets a JSON API: the resource engine emits it
    // with no enctype at all and rpc.js builds the JSON body, so submitting
    // it natively posts urlencoded (415) or cannot issue its declared
    // PUT/PATCH at all (405). Either way the user is navigated off the page
    // to a raw error and everything they typed is gone — strictly worse
    // than staying put. An application/json enctype is unsendable natively
    // for the same reason. In those cases the warning is the whole remedy.
    if (form.hasAttribute('data-fui-rpc') || form.hasAttribute('data-kiln-tool')) return;
    if ((form.getAttribute('enctype') || '').toLowerCase() === 'application/json') return;
    // Call the prototype method, not form.submit: HTML named-property
    // lookup shadows it with any control named "submit", so a form
    // carrying <button name="submit"> would throw here and the catch
    // would swallow the user's submission after we already prevented it.
    try { HTMLFormElement.prototype.submit.call(form); } catch (_) {}
  };
  // Widget-scoped listeners live in the widgets MODULE and prevent the
  // default before awaiting rpc too, so they need the same recovery — the
  // document bridge deliberately skips anything inside [data-fui-widget]
  // and cannot cover for them.
  window.__gofastr._rpcUnavailable = _rpcUnavailable;
  window.__gofastr._rpcFormFallback = _rpcFormFallback;

  if (!document.__fuiStaticDispatch && !document.__fuiGlobalDispatch) {
    document.__fuiGlobalDispatch = true;
    document.addEventListener('click', async (e) => {
      if (e.target.closest('[data-fui-widget]')) return;
      // Signal mutations win, as they did when one delegator owned both:
      // the old handler set the signal and RETURNED without consulting
      // data-fui-rpc. Two listeners would otherwise both fire on an
      // element carrying each attribute.
      if (e.target.closest('[data-fui-signal-set],[data-fui-signal-inc],[data-fui-signal-toggle]')) return;
      const node = e.target.closest('[data-fui-rpc],[data-kiln-tool]');
      if (!node || node.tagName === 'FORM') return;
      e.preventDefault();
      try {
        await loadModule('rpc');
        await window.__gofastr.dispatchRPC(node);
      } catch (_) { _rpcUnavailable(); }
    });

    document.addEventListener('submit', async (e) => {
      const form = e.target.closest('form');
      if (!form || form.closest('[data-fui-widget]')) return;
      if (form.hasAttribute('data-fui-rpc') || form.hasAttribute('data-kiln-tool')) {
        e.preventDefault();
        try {
          await loadModule('rpc');
          await window.__gofastr.dispatchRPC(form);
        } catch (_) { _rpcFormFallback(form); }
        return;
      }

      const action = form.getAttribute('action');
      if (!action || !window.__gofastr._sameOrigin(action)) return;
      const enctype = (form.getAttribute('enctype') || '').toLowerCase();
      if (enctype !== 'application/json' && !form.hasAttribute('data-fui-spa')) return;
      e.preventDefault();
      try {
        await loadModule('rpc');
        await window.__gofastr.dispatchRPC(form);
      } catch (_) { _rpcFormFallback(form); }
    });

    document.addEventListener('input', (e) => {
      // Open a focused combobox immediately. The network request remains
      // debounced in the RPC module, but the listbox should react to typing
      // before the response arrives.
      const combo = e.target && e.target.closest && e.target.closest('[role="combobox"]');
      if (combo) {
        const lbId = combo.getAttribute('aria-controls');
        const lb = lbId ? document.getElementById(lbId) : null;
        if (lb) {
          combo.setAttribute('aria-expanded', 'true');
          lb.removeAttribute('hidden');
        }
      }
      const form = e.target.closest('form[data-fui-rpc][data-fui-rpc-trigger="input"]');
      if (!form) return;
      loadModule('rpc')
        .then(() => window.__gofastr.dispatchRPC(form, 'input'))
        .catch(() => {});
    });
  }

  // Hover/focus prefetch: any element with data-fui-prefetch="<name>"
  // kicks off the module fetch as soon as the user hovers or
  // keyboard-focuses it. By the time they click, the module is
  // resolved. Capture-phase + once-per-element so we don't churn on
  // every mouse move.
  const _prefetchAttempted = new WeakSet();
  function _prefetch(e) {
    const node = e.target && e.target.closest && e.target.closest('[data-fui-prefetch]');
    if (!node || _prefetchAttempted.has(node)) return;
    _prefetchAttempted.add(node);
    const names = (node.getAttribute('data-fui-prefetch') || '').split(/\s+/).filter(Boolean);
    for (const n of names) { loadModule(n).catch(() => {}); }
  }
  document.addEventListener('pointerover', _prefetch, { capture: true, passive: true });
  document.addEventListener('focusin', _prefetch, { capture: true });

  // === DRAG-TO-DISMISS (bottom-sheet style) ============================
  // Pointer-driven drag-to-close for widgets (DragDismiss /
  // preset.BottomSheet) lives in the split-runtime module at
  // core-ui/runtime/src/dragdismiss.js — demand-loaded via the
  // [data-fui-drag-dismiss="true"] scanner below (SSR-inlined sheets
  // load at boot; dynamically-opened chrome is caught by the
  // MutationObserver scan when it's appended to <body>).

  // === DEMAND-LOAD SCANNERS ===========================================
  // Marker-driven modules are rescanned after boot, SPA navigation,
  // and DOM insertion.
  const _moduleMarkers = [
    { name: 'rpc', selector: '[data-fui-rpc],[data-kiln-tool]' },
    // Copy-to-clipboard delegated handler. Loaded when any
    // [data-fui-copy-text-from] button is on the page (or arrives via
    // SPA-nav). The src/copy.js module installs a single document-level
    // listener that handles every button.
    { name: 'copy',       selector: '[data-fui-copy-text-from]' },
    // Computed: client-side derived signals (core-ui/store). The module
    // subscribes each [data-fui-computed] node to its dependency signals
    // and recomputes via the host-registered reducer on any change.
    { name: 'computed',   selector: '[data-fui-computed]' },
    // Compute: registered same-origin Web Worker and WebAssembly assets.
    // The marker only loads the imperative __gofastr.compute API.
    { name: 'compute',    selector: '[data-fui-compute]' },
    { name: 'fileupload', selector: '[data-fui-fileupload]' },
    { name: 'popover',    selector: '[data-fui-popover-anchor]' },
    { name: 'menu',       selector: '[data-fui-menu]' },
    // Disclosure: aria-expanded mirroring, Escape-to-close, menu
    // focus-on-open, and the opt-in inert focus trap for drawers.
    { name: 'disclosure', selector: 'details[data-fui-disclosure]' },
    { name: 'toasts',     selector: '[data-fui-toast-stack],[data-fui-toast]' },
    // SSE: background event stream. Idle-loaded — never blocks first
    // interaction; the channel only carries push updates, not user
    // actions. See ROADMAP §8 Phase 5.
    { name: 'sse',        selector: 'meta[name="gofastr-sse"]', idle: true },
    // Widgets: any SSR-inlined widget element or any data-fui-open
    // trigger button anywhere on the page. The catalog auto-mount
    // path explicitly awaits loadModule('widgets') too, so this
    // scanner just covers the marker-on-page path. Idle-loaded —
    // SSR-inlined widget chrome is already on the page; mounting is
    // hydration not first paint. See ROADMAP §8 Phase 5.
    { name: 'widgets',    selector: '[data-fui-widget],[data-fui-open]', idle: true },
    // Combobox: any WAI-ARIA combobox + listbox pair. The module
    // handles keyboard nav, click-to-pick, outside-click close, and
    // updates aria-expanded + aria-activedescendant.
    { name: 'combobox',   selector: '[role="combobox"]' },
    // Tree: any WAI-ARIA tree. The module handles roving tabindex,
    // arrow-key nav, type-ahead, and toggle clicks that flip
    // aria-expanded + show/hide child <ul role="group">.
    { name: 'tree',       selector: '[role="tree"]' },
    // InfiniteScroll: wrappers with the marker attribute. The module
    // attaches an IntersectionObserver to each
    // [data-fui-infinite-sentinel] inside and POSTs to
    // data-fui-infinite-scroll.
    { name: 'infinitescroll', selector: '[data-fui-infinite-scroll]' },
    // Banner: dismissible inline-alert support. The module runs the
    // localStorage-backed hide pass for already-dismissed banners and
    // wires the delegated click handler for the X button.
    { name: 'banner',         selector: '[data-fui-banner-dismiss]' },
    // Slider: mirrors <input type="range"> value into the associated
    // <output> on input events. Loaded only when ShowValue=true (the
    // mirror marker is on the input then).
    { name: 'slider',         selector: '[data-fui-slider-mirror]' },
    // NumberInput: wires the +/- step buttons of framework/ui.NumberInput
    // to the associated <input type="number">.
    { name: 'numberinput',    selector: '[data-fui-number-step]' },
    // TextArea autogrow: applies the same auto-resize handler the
    // widget runtime uses for textareas anywhere on the page.
    { name: 'textarea',       selector: 'textarea[data-fui-autogrow]' },
    // MultiSelect: chip rendering for checked options + chip removal.
    { name: 'multiselect',    selector: '[data-fui-multiselect-chips]' },
    // FileDropzone: filename display + optional image preview strip.
    { name: 'dropzone',       selector: '[data-fui-comp="ui-dropzone"]' },
    // RangeSlider: cross-clamp min/max thumbs + optional value mirror.
    { name: 'rangeslider',    selector: 'input[data-fui-range-slider]' },
    // TagInput: commit on Enter/comma, backspace removes last, chip ×.
    { name: 'taginput',       selector: '[data-fui-tag-input]' },
    // AnimatedCounter: IntersectionObserver-driven tick on first view.
    { name: 'animatedcounter', selector: '[data-fui-animated-counter]' },
    // TableOfContents: harvest h2/h3 from target region + active-section tracking.
    { name: 'toc',             selector: '[data-fui-toc]' },
    // ScrollSpy: generic IntersectionObserver section tracking for any nav with in-page anchors.
    { name: 'scrollspy',       selector: '[data-fui-scrollspy]' },
    // OptimisticAction: SSR-declared success state flips on click, RPC fires underneath, rolls back on non-2xx.
    { name: 'optimisticaction', selector: '[data-fui-comp="ui-optimistic-action"]' },
    // ToggleAction: three-state mutex toggle (idle ↔ committed with optional untoggle, mutually exclusive within data-fui-toggle-group).
    { name: 'toggleaction', selector: '[data-fui-comp="ui-toggle-action"]' },
    // DragDismiss: pointer drag-to-close for BottomSheet-style widgets.
    { name: 'dragdismiss', selector: '[data-fui-drag-dismiss="true"]' },
    // NetworkRetryBanner: persistent banner gated by RPC-failure threshold / SSE silence. Health-check retry.
    { name: 'networkretrybanner', selector: '[data-fui-comp="ui-network-retry-banner"]' },
    // SortableList: HTML5 drag + keyboard reorder. POSTs new order on commit.
    { name: 'sortablelist',    selector: '[data-fui-sortable]' },
    { name: 'shortcut',        selector: '[data-fui-shortcut-focus],[data-fui-shortcut-click]' },
    { name: 'lightbox',        selector: '[data-fui-comp="ui-lightbox"][data-fui-lightbox]', interactions: [
      { event: 'click', selector: '[data-fui-lightbox-prev],[data-fui-lightbox-next]' },
      { event: 'keydown', scope: '[data-fui-widget]:not([hidden]) [data-fui-comp="ui-lightbox"][data-fui-lightbox]', keys: ['ArrowLeft', 'ArrowRight'] },
    ] },
    { name: 'carousel',        selector: '[data-fui-carousel]' },
    { name: 'themeswitch',     selector: '[data-fui-theme-toggle]' },
    { name: 'sidebar', selector: '[data-fui-sidebar-collapse]' },
    // BackToTop: scroll-past-threshold reveal + smooth scroll.
    { name: 'backtotop',       selector: '[data-fui-back-to-top]' },
    // ConditionalField: show/hide content based on another field's value.
    { name: 'conditionalfield', selector: '[data-fui-comp="ui-conditional-field"]' },
    // PasswordInput: show/hide toggle for password fields.
    { name: 'passwordinput',   selector: '[data-fui-comp="ui-password-input"]' },
    // SearchInput: clear button visibility + input clearing.
    { name: 'searchinput',     selector: '[data-fui-comp="ui-search-input"]' },
    // FormRepeater: serializes field values into RPC add/remove clicks.
    { name: 'formrepeater',    selector: '[data-fui-comp="ui-form-repeater"]' },
      // Dropdown: click-toggle + click-outside dismiss + Esc close.
    { name: 'dropdown',         selector: '[data-fui-dropdown-wrap]' },
    // Reveal: IntersectionObserver-driven entrance animations.
    { name: 'reveal',           selector: '[data-fui-reveal]' },
    // Animate: signal-driven CSS class toggling.
    { name: 'animate',          selector: '[data-fui-animate-signal]' },
    // PaneHost: primary pane + openable secondary/tertiary side panes
    // with a responsive overlay-drawer collapse. Wires open/close/swap
    // triggers + the focus/scroll-lock lifecycle.
    { name: 'panehost',         selector: '[data-fui-pane-host]' },
    // Poll: page-level region polling. data-fui-poll="<duration>" +
    // data-fui-poll-src="<url>" re-fetches the URL on the cadence and
    // swaps the response HTML into the element. The module owns
    // parse/clamp/jitter/pause/back-off/teardown; core only loads it.
    { name: 'poll',         selector: '[data-fui-poll]' },
];

  // Demand-loaded modules may declare interactions that must survive their
  // own cold-cache fetch. This bridge is deliberately metadata-driven: core
  // owns event retention/replay, while feature modules own selectors and
  // behavior. No optional component's selectors or policy are hard-coded in
  // the always-loaded runtime.
  const _interactionReplay = new WeakSet();
  const _warnModuleUnavailable = (name) => {
    console.warn('[gofastr] ' + name + ' module unavailable — retrying may help');
  };
  const _interactionNode = (e, spec) => {
    if (spec.event === 'keydown') {
      if (!spec.keys || !spec.keys.includes(e.key) ||
          !document.querySelector(spec.scope || '')) return null;
      return e.target && e.target.dispatchEvent ? e.target : document.body;
    }
    return e.target && e.target.closest && e.target.closest(spec.selector);
  };
  const _replayInteraction = (e, node) => {
    const init = { bubbles: true, cancelable: true, composed: true };
    if (e.type === 'click') {
      init.view = window;
      init.detail = e.detail;
      init.button = e.button;
      init.buttons = e.buttons;
      init.clientX = e.clientX;
      init.clientY = e.clientY;
    } else if (e.type === 'keydown') {
      init.key = e.key;
      init.code = e.code;
      init.location = e.location;
      init.repeat = e.repeat;
      init.ctrlKey = e.ctrlKey;
      init.altKey = e.altKey;
      init.shiftKey = e.shiftKey;
      init.metaKey = e.metaKey;
    }
    let replay;
    try {
      replay = new e.constructor(e.type, init);
    } catch (_) {
      replay = new Event(e.type, init);
    }
    node.dispatchEvent(replay);
  };
  for (const marker of _moduleMarkers) {
    for (const spec of marker.interactions || []) {
      document.addEventListener(spec.event, async (e) => {
        const node = _interactionNode(e, spec);
        if (!node || _interactionReplay.has(node) ||
            window.__gofastr.loadedModules?.[marker.name]) return;
        e.preventDefault();
        try {
          await loadModule(marker.name);
          _interactionReplay.add(node);
          try { _replayInteraction(e, node); }
          finally { _interactionReplay.delete(node); }
        } catch (_) {
          _warnModuleUnavailable(marker.name);
        }
      });
    }
  }

  function _scanForModules(root) {
    const scope = root && root.querySelectorAll ? root : document;
    const idleQueue = [];
    for (const m of _moduleMarkers) {
      const { name, selector, idle } = m;
      // rpc-stub owns static-export clicks. The marker table is shared by all
      // compositions, so skip this one entry instead of fetching dead code.
      if (name === 'rpc' && document.__fuiStaticDispatch) continue;
      // Skip if the module is already loaded — its own internal scanner
      // takes care of newly inserted DOM via the MutationObserver.
      if (window.__gofastr.loadedModules?.[name]) continue;
      // Test the scope node ITSELF as well as its descendants: a
      // lazily-mounted widget root appended to <body> carries root
      // markers (data-fui-drag-dismiss) on the node handed to us.
      if (!(scope.matches?.(selector) || scope.querySelector(selector))) continue;
      if (idle) {
        idleQueue.push(name);
      } else {
        loadModule(name).catch(() => {});
      }
    }
    if (idleQueue.length) _scheduleIdleModules(idleQueue);
  }
  // Phase 5 idle fallback (ROADMAP §8). Modules tagged `idle: true` in
  // `_moduleMarkers` ship after FCP via requestIdleCallback so they
  // never compete with the user's first interaction. Safari < 16.2 and
  // Firefox < 55 lack rIC — fall back to setTimeout(0) which still
  // runs after the current task settles.
  function _scheduleIdleModules(names) {
    const rIC = window.requestIdleCallback || ((fn) => setTimeout(fn, 0));
    rIC(() => {
      for (const n of names) loadModule(n).catch(() => {});
    });
  }
  // Re-scan after SPA-nav swaps content. Two phases:
  //
  //  1. Marker scan — modules that AREN'T loaded yet get fetched when
  //     their marker appears in the freshly-swapped content. (Fresh
  //     page brings new feature → load on demand.)
  //
  //  2. Per-module rescan — modules that ARE loaded re-run their
  //     scanner against the new DOM. Modules opt in by registering
  //     a function on `window.__gofastr._moduleScanners[name]`; the
  //     contract is "wire any new elements inside `root`, idempotent
  //     against already-wired elements". This is how SSR-inlined
  //     toast stacks on the new page get their TTL timers armed —
  //     without it, `_initToasts` would have run only once at module
  //     load before that DOM existed.
  window.addEventListener('gofastr:navigate', () => {
    _scanForModules(document);
    // Task A: re-inject aria-live onto any new signal nodes from the swapped page.
    _injectSignalAria();
    const G = window.__gofastr;
    if (G && G._moduleScanners) {
      for (const name in G._moduleScanners) {
        if (G.loadedModules && G.loadedModules[name]) {
          try { G._moduleScanners[name](document); } catch (_) {}
        }
      }
    }
  });

  // Close any open modal widgets on SPA navigation. Toasts/panels
  // (non-backdrop'd widgets) survive — they're page-independent
  // UI like build-progress banners.
  window.addEventListener('gofastr:navigate', () => {
    const G = window.__gofastr;
    if (!G || !G._modalStack) return;
    for (const name of [...G._modalStack]) G.closeWidget(name);
  });

  // Re-fetch the widget catalog after SPA-nav so page-scoped widgets
  // registered with .Pages("/route") become available when the user
  // arrives via partial-fetch (instead of a full page load).
  //
  // Without this, the boot-time catalog only contains widgets visible
  // on the initial path; clicking a data-fui-open trigger for a
  // page-scoped widget elsewhere silently bails because the entry is
  // missing from _widgetCatalog.
  //
  // The fetch is idempotent — entries are MERGED into the catalog
  // (existing entries from boot don't get overwritten unless the
  // server returns a changed version). Non-hidden widgets that
  // aren't already mounted are mounted now. Then _syncDeepLinks runs
  // so the URL's modal/drawer query params open the right surface.
  window.addEventListener('gofastr:navigate', (e) => {
    const path = (e && e.detail && e.detail.path) || location.pathname;
    fetch('/__gofastr/widgets?page=' + encodeURIComponent(path),
          { headers: { 'X-Gofastr-Widget-Discovery': '1' } })
      .then((r) => (r.ok ? r.json() : null))
      .then(async (list) => {
        if (!Array.isArray(list) || list.length === 0) return;
        const G = window.__gofastr;
        if (!G) return;
        // Make sure the widgets module is loaded — the initial page
        // may have had no widgets, so loadModule('widgets') was never
        // triggered and mountWidget isn't on the namespace yet.
        try { await G.loadModule('widgets'); } catch (_) { return; }
        G._widgetCatalog = G._widgetCatalog || {};
        for (const item of list) {
          const cfg = item.cfg;
          G._widgetCatalog[cfg.name] = item;
          // Auto-mount non-hidden widgets that aren't already on the
          // page. Hidden widgets (Modal / Drawer / Popover) stay
          // hidden until openWidget is called from a trigger.
          if (item.hidden) continue;
          if (G._mountByName) G._mountByName(cfg.name);
        }
        if (G._syncDeepLinks) G._syncDeepLinks();
      })
      .catch(() => { /* navigation succeeded; missing catalog is non-fatal */ });
  });

  const _bootstrapComponentCSS = () => {
    const G = window.__gofastr;
    if (!G?.scanAndLoadCSS) return;
    // Seed _pendingLinks with names already covered by the SSR
    // bundle link, so the on-demand scanner doesn't redundantly load
    // per-component sheets. The names live on the bundle <link>'s
    // data-fui-bundle attribute (a stable contract), not parsed
    // from the URL.
    document.head.querySelectorAll('link[data-fui-bundle]').forEach((l) => {
      const names = (l.getAttribute('data-fui-bundle') || '').split(',');
      for (const n of names) if (n) G._pendingLinks.add(n);
    });
    G.scanAndLoadCSS(docEl);
    G.scheduleIdleLoads();
  };

  // Event listeners attach unconditionally — they fire only when the
  // matching event happens, so installing them before the DOM is parsed
  // is safe. An earlier arrangement gated them inside
  // `if (document.readyState === 'loading')`, which silently disabled
  // them when runtime.js loaded after DOMContentLoaded (late injection,
  // fast parse, dynamic re-init).

  // Disclosure keyboard/AT behaviour — aria-expanded mirroring,
  // Escape-to-close, menu focus-on-open, and the opt-in focus trap —
  // lives in the split-runtime module at core-ui/runtime/src/disclosure.js,
  // demand-loaded via the details[data-fui-disclosure] scanner below.
  // Core keeps only the close-on-navigate lines; the `toggle` event they
  // raise is what the module reacts to.

  // Task A: auto-inject aria-live onto signal nodes so screen readers
  // announce dynamic updates. Restricted to TEXT-mode nodes (the default
  // when data-fui-signal-mode is absent or "text"): attr-mode and
  // html-mode bindings must NOT receive role=status because:
  //  - attr-mode: injects into element attributes (e.g. <a href=…>),
  //    not text — role=status on an <a> is invalid ARIA.
  //  - html-mode: swaps innerHTML of island wrappers; treating the
  //    entire region as a live region causes a storm of announcements
  //    on every island update. Those regions use their own role/aria.
  // Runs at boot and after SPA navigation.
  const _injectSignalAria = () => {
    document.querySelectorAll('[data-fui-signal]').forEach((node) => {
      const mode = node.getAttribute('data-fui-signal-mode') || 'text';
      if (mode !== 'text') return;
      if (!node.getAttribute('role')) node.setAttribute('role', 'status');
      if (!node.getAttribute('aria-live')) node.setAttribute('aria-live', 'polite');
      if (!node.getAttribute('aria-atomic')) node.setAttribute('aria-atomic', 'true');
    });
  };
  // Initial-pass hooks: these scan the CURRENT DOM, so they have
  // to wait until the document is at least parsed. updateActiveLink
  // marks server-rendered nav links; _bootstrapComponentCSS scans
  // existing markers; _scanForModules dispatches demand-load
  // modules (the disclosure module is one of them, and does its own
  // aria-expanded sync for server-rendered <details>).
  // _runMountActions fires component actions marked data-action-mount once,
  // right after hydration. Component clientJS handlers (data-action) only run
  // on user events (click/input/change/submit); a server-rendered island that
  // must populate itself on load — an entity list fetching its rows, a detail
  // view fetching one record, a relation <select> fetching its options — opts
  // in by carrying data-action-mount="<actionName>" on a node inside a
  // [data-component]. Re-runs on SPA nav so a swapped-in page repopulates.
  const _runMountActions = (root) => {
    const G = window.__gofastr;
    if (!G || !G.trigger) return;
    const scope = root || document;
    for (const el of scope.querySelectorAll('[data-action-mount]')) {
      const action = el.getAttribute('data-action-mount');
      if (!action) continue;
      const componentId = closestAttr(el, 'data-component') ?? closestAttr(el, 'data-widget');
      if (!componentId) continue;
      G.trigger(componentId, action, collectParams(el));
    }
  };
  window.addEventListener('gofastr:navigate', () => _runMountActions(document));

  const _initialPass = () => {
    // nav is optional in a composition — the `embed` bundle omits it, which is
    // how SPA navigation is disabled inside frames. A bare call would throw a
    // ReferenceError here and take the whole initial pass down with it, so the
    // one nav symbol boot needs is probed rather than assumed. typeof on an
    // undeclared identifier is the only safe probe; `updateActiveLink !==
    // undefined` would itself throw.
    if (typeof updateActiveLink === 'function') updateActiveLink(location.pathname);
    _bootstrapComponentCSS();
    _scanForModules(document);
    // Intercepting routes are rare, so their module is demand-loaded off
    // the manifest: no intercepting route, no bytes, no listeners.
    if (Array.isArray(window.__gofastr_routes) &&
        window.__gofastr_routes.some((r) => r.intercept)) loadModule('intercept');
    _injectSignalAria();
    _runMountActions(document);
  };
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', _initialPass);
  } else {
    _initialPass();
  }
