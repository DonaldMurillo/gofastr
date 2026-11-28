
(function() {
  var TOKEN = document.querySelector('meta[name="theme-edit-token"]').content;
  var OUT = document.querySelector('meta[name="theme-edit-out"]').content;
  var frame = document.getElementById('te-frame');
  var statusEl = document.getElementById('te-status');
  var contrastEl = document.getElementById('te-contrast');
  var schemeBtn = document.getElementById('te-scheme');
  var writeBtn = document.getElementById('te-write');
  var currentScheme = 'light';
  var pendingTimers = {};
  // pendingValues mirrors the value each pending debounce timer will
  // apply, so a Write that races the debounce can flush the in-flight
  // edit instead of emitting the theme without its last keystroke.
  var pendingValues = {};
  var applyQueue = Promise.resolve();
  // pendingError holds the most recent apply failure (validation or
  // transport). It is cleared by a successful apply and read by
  // flushPendingEdits: a racing Write must propagate the failure and
  // block /__theme/writeback, instead of resolving the queue as if all
  // was well and overwriting the file with the previous theme.
  var pendingError = null;

  function setStatus(msg, kind) {
    // Status lives inside a fui-callout. The variant class drives the colour
    // via the design system's --color-* tokens, so kind is mapped to a
    // Callout variant rather than a bespoke .te-status--<kind> rule.
    var variant = 'neutral';
    if (kind === 'ok') variant = 'success';
    else if (kind === 'err') variant = 'danger';
    var body = statusEl.querySelector('.fui-callout__body');
    if (body) body.textContent = msg || '';
    statusEl.className = 'fui-callout fui-callout--' + variant;
  }

  function authHeaders() {
    return { 'Authorization': 'Bearer ' + TOKEN, 'Content-Type': 'application/json' };
  }

  // Collect the server-bound value from a control input.
  function readControl(input) {
    var type = input.dataset.type;
    var val = input.value;
    if (type === 'number-px') return val + 'px';
    return val;
  }

  function findDataElement(selector, attr, key) {
    var nodes = document.querySelectorAll(selector);
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].getAttribute(attr) === key) return nodes[i];
    }
    return null;
  }

  function findTextInput(key) {
    return findDataElement('[data-token]:not([data-type="color-swatch"])', 'data-token', key);
  }

  function errNodeFor(input) {
    // The reserved error paragraph: ui.FormField rendered it with the
    // id derived from the control's own (ReserveError), already wired
    // into the input's aria-describedby.
    return document.getElementById(input.id + '-error');
  }

  function showError(key, msg) {
    var input = findTextInput(key) || findDataElement('[data-token]', 'data-token', key);
    if (!input) return;
    // The field's error state, on the control itself: aria-invalid is
    // what the stylesheet colours the border from, and what the
    // reserved error node's contract asks the filling caller to set.
    input.setAttribute('aria-invalid', 'true');
    var errEl = errNodeFor(input);
    if (errEl) errEl.textContent = msg || 'invalid';
  }

  function clearError(key) {
    var input = findTextInput(key);
    if (!input) return;
    input.removeAttribute('aria-invalid');
    var errEl = errNodeFor(input);
    if (errEl) errEl.textContent = '';
  }

  // Apply one token edit: POST /__theme/apply, swap the preview's app.css
  // link to the returned variant hash, re-run the contrast check. No iframe
  // reload: swapping the link href re-fetches only app.css and the browser
  // re-resolves every var() reference against the new :root values.
  function applyEdit(key, value) {
    return fetch('/__theme/apply', {
      method: 'POST',
      headers: authHeaders(),
      body: JSON.stringify({ key: key, value: value })
    }).then(function(r) { return r.json(); }).then(function(data) {
      if (data.error) {
        showError(key, data.error);
        // Name the offending token in the status. The server's error string
        // alone ("invalid color") doesn't identify the control, so the
        // operator would have to scan every field to find the bad one.
        var msg = key + ': ' + data.error;
        setStatus(msg, 'err');
        // REJECT so a racing Write (flushPendingEdits) sees the failure and
        // blocks /__theme/writeback. Resolving normally here, the previous
        // behaviour, let the Write handler overwrite the file with the
        // PREVIOUS theme while the control still showed the typed-but-
        // rejected value. The error carries themeValidation so the Write
        // handler knows applyEdit already set the status (don't overwrite).
        var err = new Error(msg);
        err.themeValidation = true;
        throw err;
      }
      // A successful apply clears any prior failure: the working theme on
      // the server now matches what the operator sees, so a subsequent Write
      // is safe to proceed.
      pendingError = null;
      clearError(key);
      setStatus('updated ' + key, 'ok');
      swapPreviewCSS(data.hash, function() { runContrastCheck(); });
    });
    // No .catch here: the rejection must propagate so queueApply can record
    // it in pendingError. The previous .catch swallowed every error, which
    // is why a failed apply certified the previous theme as if it had
    // landed.
  }

  function queueApply(key, value) {
    applyQueue = applyQueue.then(function() {
      return applyEdit(key, value);
    }).catch(function(e) {
      // Absorb the rejection into applyQueue so the NEXT keystroke's apply
      // can still fire: without this, one bad value would poison the queue
      // for the rest of the session. Record the failure in pendingError so
      // a racing Write sees it and blocks the writeback.
      if (e && e.themeValidation) {
        pendingError = e;
        return;
      }
      // Genuine transport error (fetch threw). Wrap so the Write handler's
      // status is honest about what went wrong.
      var wrapped = new Error('apply ' + key + ' failed: ' + e);
      wrapped.themeApply = true;
      setStatus(wrapped.message, 'err');
      pendingError = wrapped;
    });
  }

  // Swap the preview iframe's app.css link to the variant URL. The link is
  // the same /__gofastr/app.css path production uses; the ?t=<hash> selects
  // the RegisterThemeVariant content. Component CSS already loaded in the
  // iframe uses var(--*) refs, so it re-resolves without a re-fetch.
  // Swaps the preview stylesheet, then runs the callback once the NEW sheet
  // has actually applied.
  //
  // Swapping an href starts a fetch; computed styles keep reporting the old
  // sheet until it lands. Measuring immediately after the swap therefore
  // measured the PREVIOUS theme: so an edit that broke contrast was checked
  // against the values it replaced and reported clean. The retry loop could not
  // see it either: the old sheet is perfectly measurable, just wrong.
  // lastRequestedHash is the variant hash the most recent swapPreviewCSS
  // asked the iframe to load. appliedHash is the hash whose sheet has
  // actually applied (signalled by the link's load event). The check uses
  // the two together to tell which generation it measured: the readiness
  // sentinel only proves "a sheet applied" and reads identical
  // black-on-white for every theme, so without these the panel could not
  // tell a stale reading (the OLD theme still applied during a slow swap)
  // from a current one.
  var lastRequestedHash = null;
  var appliedHash = null;
  function swapPreviewCSS(hash, then) {
    var doc = frame.contentDocument;
    if (!doc) return;
    var link = doc.querySelector('link[href^="/__gofastr/app.css"]');
    if (!link) return;
    var href = '/__gofastr/app.css?t=' + encodeURIComponent(hash);
    lastRequestedHash = hash;
    if (link.getAttribute('href') === href) {
      // Already showing this exact sheet: record the generation and fire.
      appliedHash = hash;
      if (then) then();
      return;
    }
    if (then) {
      // Two paths fire the callback:
      //   1. the link's load event: the new sheet has landed and applied;
      //   2. a 1.5s fallback: covers cached sheets that apply in some
      //      browsers without dispatching load.
      //
      // The previous code locked out the load event after whichever fired
      // first (done = true). On a throttled connection the fallback fired
      // first, the late load was ignored, and the contrast check measured
      // the PREVIOUS theme and reported clean for the new one. The load
      // listener MUST stay armed past the fallback so a late load re-fires
      // the callback: and the load event is the only path that updates
      // appliedHash, so checkScheme can distinguish stale from current.
      var timer = setTimeout(function() {
        // Cached-sheet workaround. The new sheet may have applied without
        // a load event; fire the callback so the check runs. We deliberately do
        // NOT set appliedHash=hash here: on a slow network the new sheet
        // has not applied yet and stamping it would let checkScheme publish
        // a reading of the OLD sheet. If this is the cached-sheet case the
        // load event will fire too and stamp the hash; if it is the
        // slow-network case the late load stamps the hash when it lands.
        then();
      }, 1500);
      link.addEventListener('load', function() {
        clearTimeout(timer);
        appliedHash = hash;  // the load event is the source of truth
        then();  // always re-run: late loads must trigger a fresh check
      }, { once: true });
      link.addEventListener('error', function() {
        clearTimeout(timer);
        // Fetch failed: the OLD sheet is still applied. Don't update
        // appliedHash. Re-run the check so it doesn't stay frozen on a
        // reading from the previous swap.
        then();
      }, { once: true });
    }
    link.setAttribute('href', href);
  }

  // Debounced input handler: collects the edited token and sends it after a
  // short quiet period so rapid typing doesn't flood the server.
  function onControlInput(input) {
    var key = input.dataset.token;
    var value;
    if (input.dataset.type === 'color-swatch') {
      // The headless behaviour module owns the swatch→text sync: it
      // writes the picked hex (uppercased) into the text input — the
      // source of truth — and clears the shell's invalid mark. What
      // is left here is the apply, from the same hex the sync is
      // writing, uppercased to match it. Which listener runs first
      // cannot change the value applied.
      value = input.value.toUpperCase();
    } else {
      value = readControl(input);
    }
    pendingValues[key] = value;
    clearTimeout(pendingTimers[key]);
    pendingTimers[key] = setTimeout(function() {
      queueApply(key, value);
      delete pendingTimers[key];
      delete pendingValues[key];
    }, 300);
  }

  // Flush every debounced edit and wait for the apply queue to drain. The Go
  // writeBack reads only s.working, so a Write that beat the 300 ms debounce
  // emitted the theme WITHOUT the operator's last keystroke: the value
  // typed into the control silently never reached the file. Flushing the
  // timers fires the queued applies, and awaiting applyQueue guarantees they
  // have landed in s.working before the writeback POST leaves the browser.
  //
  // If any apply in the flushed batch FAILED (validation or transport), the
  // returned promise rejects with pendingError. The Write handler must not
  // POST /__theme/writeback in that case: the working theme on the server
  // does not match what the operator sees, so writing it would silently
  // discard the value they just typed.
  function flushPendingEdits() {
    var keys = Object.keys(pendingTimers);
    for (var i = 0; i < keys.length; i++) {
      var k = keys[i];
      var id = pendingTimers[k];
      if (!id) { delete pendingTimers[k]; continue; }
      clearTimeout(id);
      delete pendingTimers[k];
      if (pendingValues[k] !== undefined) {
        queueApply(k, pendingValues[k]);
        delete pendingValues[k];
      }
    }
    return applyQueue.then(function() {
      // applyQueue always resolves (queueApply absorbs rejections into
      // pendingError). Surface the failure here so the Write handler's
      // .catch fires instead of its .then → /__theme/writeback.
      if (pendingError) throw pendingError;
    });
  }

  // ---- Scheme toggle ----------------------------------------------------
  function setScheme(scheme) {
    currentScheme = scheme;
    schemeBtn.textContent = scheme === 'dark' ? '◑ Dark' : '◐ Light';
    var doc = frame.contentDocument;
    if (doc && doc.documentElement) {
      doc.documentElement.setAttribute('data-color-scheme', scheme);
    }
    runContrastCheck();
  }

  schemeBtn.addEventListener('click', function() {
    setScheme(currentScheme === 'dark' ? 'light' : 'dark');
  });

  // ---- Write-back -------------------------------------------------------
  writeBtn.addEventListener('click', function() {
    writeBtn.disabled = true;
    setStatus('writing ' + OUT + ' …');
    flushPendingEdits().then(function() {
      return fetch('/__theme/writeback', { method: 'POST', headers: authHeaders(), body: '{}' });
    })
      .then(function(r) { return r.json(); })
      .then(function(data) {
        writeBtn.disabled = false;
        if (data.error) { setStatus(data.error, 'err'); return; }
        setStatus('wrote ' + (data.path || OUT), 'ok');
      })
      .catch(function(e) {
        writeBtn.disabled = false;
        // A validation/apply failure already named the offending token in
        // setStatus (applyEdit or queueApply wrote the message). Overwriting
        // it with "network error" would hide which token was invalid.
        if (e && (e.themeValidation || e.themeApply)) return;
        setStatus('write blocked: ' + e, 'err');
      });
  });

  // ---- Contrast check (browser-side, every colour space) ----------------
  // getComputedStyle resolves oklch(), color-mix(), var() etc. natively;
  // the repo's only Go-side contrast helper is hex-only and returns 0 for
  // the oklch values real themes use. We check BOTH schemes by flipping the
  // iframe's data-color-scheme and reading the probe elements.
  // Converts ANY CSS colour the browser understands into sRGB bytes.
  //
  // Hand-parsing three numbers out of the computed value is wrong twice over.
  // getComputedStyle does not promise rgb(): Chromium hands back oklch(...)
  // and oklab(...) verbatim: so "oklch(1 0 0)" (white) parsed as [1,0,0]
  // (nearly black) and a 21:1 pair was reported as failing at 1.00:1. And the
  // theme's own values ARE oklch, which is the whole reason this check runs in
  // a browser instead of in Go.
  //
  // Painting one pixel delegates colour-space parsing to the browser. Two
  // fillStyle sentinels distinguish an invalid assignment from every valid
  // spelling of black.
  var _cvx = null;
  function toRGB(color) {
    var v = String(color).trim();
    if (!v) return null;
    if (!_cvx) {
      var c = document.createElement('canvas');
      c.width = c.height = 1;
      _cvx = c.getContext('2d', { willReadFrequently: true });
    }
    if (!_cvx) return null;
    _cvx.fillStyle = '#010203';
    _cvx.fillStyle = v;
    var first = _cvx.fillStyle;
    _cvx.fillStyle = '#040506';
    _cvx.fillStyle = v;
    if (_cvx.fillStyle !== first) return null;

    _cvx.clearRect(0, 0, 1, 1);
    _cvx.fillStyle = v;
    _cvx.fillRect(0, 0, 1, 1);
    var d = _cvx.getImageData(0, 0, 1, 1).data;
    return [d[0], d[1], d[2], d[3] / 255];
  }
  function parseRgb(color) { return toRGB(color); }

  // Porter-Duff source-over compositing. Both inputs are [r,g,b,a].
  function composite(top, bottom) {
    var alpha = top[3] + bottom[3] * (1 - top[3]);
    if (alpha === 0) return [0, 0, 0, 0];
    var scale = bottom[3] * (1 - top[3]);
    return [
      (top[0] * top[3] + bottom[0] * scale) / alpha,
      (top[1] * top[3] + bottom[1] * scale) / alpha,
      (top[2] * top[3] + bottom[2] * scale) / alpha,
      alpha
    ];
  }

  // WCAG relative luminance: sRGB channel to linear, then the standard weights.
  function channel(v) {
    var c = v / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  }
  function luminance(rgb) { return 0.2126 * channel(rgb[0]) + 0.7152 * channel(rgb[1]) + 0.0722 * channel(rgb[2]); }
  function ratio(fg, bg, behind) {
    var a = parseRgb(fg), b = parseRgb(bg);
    if (!a || !b) return null;
    var renderedBg = composite(b, behind || [255, 255, 255, 1]);
    var renderedFg = composite(a, renderedBg);
    var l1 = luminance(renderedFg), l2 = luminance(renderedBg);
    var hi = Math.max(l1, l2), lo = Math.min(l1, l2);
    return (hi + 0.05) / (lo + 0.05);
  }
  // Returns null when the stylesheet has not applied yet, so the caller waits
  // instead of publishing a reading it does not have.
  //
  // Readiness is decided by ONE sentinel probe whose colours are literals, not
  // var() references. A pair that resolves transparent is a real answer (the
  // element inherits the page background), not evidence of a missing sheet:
  // treating it as the latter aborted the whole check whenever any scheme had
  // a transparent background, and the panel then kept whatever it last showed.
  function checkScheme(scheme) {
    var doc = frame.contentDocument;
    if (!doc) return null;
    doc.documentElement.setAttribute('data-color-scheme', scheme);
    var probes = doc.querySelectorAll('[data-cp]');
    if (!probes.length) return null;

    // Verify the iframe is showing the generation we asked for. The
    // readiness sentinel below only proves "some sheet applied": it reads
    // literal black-on-white under BOTH the old and new themes, so without
    // this guard the panel measured the PREVIOUS theme's already-applied
    // sheet during a slow swap and certified the old values as the new
    // ones. lastRequestedHash is set the moment swapPreviewCSS starts the
    // fetch; appliedHash is set only when the link's load event fires, so a
    // mismatch means the new sheet is still in flight.
    if (lastRequestedHash && appliedHash !== lastRequestedHash) return null;


    var win = frame.contentWindow;
    var sentinel = doc.querySelector('[data-cp="ready|sentinel"]');
    if (!sentinel) return null;
    var sc = win.getComputedStyle(sentinel);
    // The sentinel declares literal black on literal white with no var()
    // references, so once the generated stylesheet has applied it MUST read
    // back as exactly that. Testing for a non-null ratio tested nothing:
    // getComputedStyle always yields a parseable colour, so an unstyled
    // document measured as inherited black over transparent, composited to
    // ~21:1, and the panel reported a clean bill of health for a theme it had
    // never measured. It also made the retry loop below unreachable.
    if (sc.color !== 'rgb(0, 0, 0)' || sc.backgroundColor !== 'rgb(255, 255, 255)') return null;

    // The browser's canvas is transparent. Resolve the root over white, then
    // the body over that root. Probe backgrounds composite over this measured
    // page background; foregrounds composite over the resulting probe fill.
    var transparent = [0, 0, 0, 0];
    var white = [255, 255, 255, 1];
    var rootBg = parseRgb(win.getComputedStyle(doc.documentElement).backgroundColor) || transparent;
    var bodyBg = parseRgb(win.getComputedStyle(doc.body).backgroundColor) || transparent;
    var pageBg = composite(bodyBg, composite(rootBg, white));

    var fails = [];
    for (var i = 0; i < probes.length; i++) {
      var p = probes[i];
      var name = p.getAttribute('data-cp');
      if (name === 'ready|sentinel') continue;
      var cs = win.getComputedStyle(p);
      var bg = cs.backgroundColor;
      var r = ratio(cs.color, bg, pageBg);
      if (r === null) {
        // Report it. Dropping a pair the browser served in a form we could not
        // convert is how a checker quietly shrinks to the subset it happens to
        // understand and still says "all clear".
        fails.push({ name: name, scheme: scheme, ratio: null, why: cs.color + ' on ' + bg });
        continue;
      }
      if (r < 4.5) fails.push({ name: name, scheme: scheme, ratio: r });
    }
    return fails;
  }
  // Retries until the probes are actually measurable rather than trusting a
  // fixed delay. The stylesheet the probes depend on is fetched by the frame,
  // and a swap re-fetches it; measuring on a timer meant sometimes measuring an
  // unstyled document and publishing the result as fact.
  function runContrastCheck(attempt) {
    try {
      runContrastCheckInner(attempt);
    } catch (e) {
      // Never fail silently. A hidden panel reads as "no contrast problems",
      // so an exception in here does not just lose the check: it asserts a
      // clean bill of health. Say what happened instead.
      renderContrastError(String((e && e.message) || e));
    }
  }
  function runContrastCheckInner(attempt) {
    attempt = attempt || 0;
    var doc = frame.contentDocument;
    if (!doc) return;
    var light = checkScheme('light');
    var dark = light === null ? null : checkScheme('dark');
    if (light === null || dark === null) {
      doc.documentElement.setAttribute('data-color-scheme', currentScheme);
      if (attempt < 20) setTimeout(function() { runContrastCheck(attempt + 1); }, 100);
      else renderContrastError('probes never became measurable');
      return;
    }
    var fails = light.concat(dark);
    // Restore the user's selected scheme.
    doc.documentElement.setAttribute('data-color-scheme', currentScheme);
    renderContrast(fails);
  }
  function renderContrast(fails) {
    if (fails.length === 0) {
      contrastEl.hidden = true;
      contrastEl.innerHTML = '';
      return;
    }
    contrastEl.hidden = false;
    var items = fails.map(function(f) {
      if (f.ratio === null) {
        return '<li>' + f.scheme + ': ' + htmlEsc(f.name) + ' — not measurable (' + htmlEsc(f.why || '') + ')</li>';
      }
      return '<li>' + f.scheme + ': ' + htmlEsc(f.name) + ' — ' + f.ratio.toFixed(2) + ':1</li>';
    });
    contrastEl.innerHTML = '<h4>Contrast findings (' + fails.length + ')</h4><ul>' + items.join('') + '</ul>';
  }
  // A broken check must LOOK broken. Reusing the hidden state for "nothing to
  // report" and for "the check did not run" is what let this surface report a
  // clean bill of health for every theme it was ever pointed at.
  function renderContrastError(msg) {
    contrastEl.hidden = false;
    contrastEl.innerHTML = '<h4>Contrast check unavailable</h4><ul><li>' + htmlEsc(msg) + '</li></ul>';
  }
  function htmlEsc(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

  // ---- Wire up controls -------------------------------------------------
  document.querySelectorAll('[data-token]').forEach(function(el) {
    el.addEventListener('input', function() { onControlInput(el); });
  });

  // Run the contrast check once the iframe has loaded its first paint.
  // Adopt the working theme when the frame loads.
  //
  // The preview page is server-rendered with the app theme; the working theme
  // lives in a registered variant. Without this a reload showed edited values in
  // every control beside a preview that had silently reverted: the same
  // stale-state bug as rendering the controls from the base theme, one level
  // out.
  frame.addEventListener('load', function() {
    var v = document.querySelector('meta[name="theme-edit-variant"]');
    var key = v ? v.getAttribute('content') : '';
    if (key) swapPreviewCSS(key, function() { runContrastCheck(); });
    else runContrastCheck();
  });
})();
