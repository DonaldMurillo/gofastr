  // ---------------------------------------------------------------------
  // boot-embed — the frame side of an embedded surface.
  //
  // Ships ONLY in the `embed` composition (core-ui/runtime/runtime.go
  // composeEmbed). It is inert without a <meta name="gofastr-embed"> config,
  // so composing it into a document that is not an embed shell does nothing.
  //
  // What it owns:
  //   - the postMessage handshake that brings the single-use nonce into the
  //     frame (the nonce is never in the frame URL, where Referer, history and
  //     the customer's analytics would all see it);
  //   - exchanging that nonce for a grant, and refreshing the grant while the
  //     frame lives;
  //   - fetching the surface's server-rendered content AS the granted subject
  //     and injecting it (the shell document itself is anonymous — it is
  //     fetched by a navigation, which can carry no credential);
  //   - reporting height to the parent so the iframe can size itself;
  //   - contributing the grant header to every runtime fetch via
  //     window.__gofastr._extraHeaders.
  //
  // What it deliberately does NOT own: navigation. The `embed` composition
  // omits the nav fragment entirely, so SPA navigation inside a frame is
  // impossible by absence rather than by a flag someone can flip back.
  // ---------------------------------------------------------------------
  (() => {
    const meta = document.querySelector('meta[name="gofastr-embed"]');
    if (!meta) return;

    let cfg;
    try { cfg = JSON.parse(meta.getAttribute('content') || '{}'); }
    catch (_) { return; }
    if (!cfg || !cfg.surface || !cfg.content || !cfg.exchange) return;

    const root = document.getElementById('gofastr-embed-root');
    const PROTO = 'gofastr-embed/1';
    const REQUEST_TIMEOUT_MS = 15000;

    // The parent's origin, as attested by the browser on the message event.
    // A page cannot forge event.origin, so this is a trustworthy record of
    // WHO framed us — but it is only trustworthy in a browser, which is why
    // the server treats it as corroboration and lets CSP frame-ancestors do
    // the actual enforcing.
    let parentOrigin = null;
    let grant = null;
    let refreshTimer = null;
    let refreshFailures = 0;
    let started = false;

    const fail = (code, detail) => {
      if (root) {
        root.setAttribute('data-fui-embed-state', 'error');
        root.textContent = 'This panel could not load. Error: ' + code + '.';
      }
      post({ type: 'error', code: code });
      console.warn('[gofastr/embed] ' + code, detail || '');
    };

    // The credential is done and cannot be renewed.
    //
    // The grant is deliberately NOT cleared. Clearing it makes the wrapper stop
    // sending the header, and a request with no header is not a refused embed
    // request — it is an ordinary anonymous one, which the server passes
    // straight through. A dashboard polling every 30s would then quietly swap
    // its authenticated numbers for the logged-out render, with the frame still
    // reporting state "ready". Keeping the dead token makes the server answer
    // 401, which is a thing the parent and the console can both see.
    //
    // Content already on screen is left alone: it was authentic when it
    // rendered, and blanking a panel because a token aged out loses the user
    // their view for no safety gain.
    const expire = () => {
      if (root) root.setAttribute('data-fui-embed-state', 'expired');
      post({ type: 'expired' });
      console.warn('[gofastr/embed] grant expired — reload the host page for a fresh nonce');
    };

    function post(msg) {
      if (window.parent === window) return;
      msg.proto = PROTO;
      // Before the handshake we do not know the parent's origin, so the
      // "ready" ping goes to '*'. It carries nothing but a protocol marker;
      // every message that carries state waits for parentOrigin.
      const target = parentOrigin || (msg.type === 'ready' ? '*' : null);
      if (!target) return;
      try { window.parent.postMessage(msg, target); } catch (_) {}
    }

    async function withDeadline(task) {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
      try {
        return await task(controller.signal);
      } finally {
        clearTimeout(timeout);
      }
    }

    // ---- height reporting -------------------------------------------------
    let heightPending = false;
    let lastHeight = -1;
    // Measure the CONTENT, never the viewport.
    //
    // The document's own scroll height is at least the viewport height, and
    // the viewport here IS the frame the host sizes from our report — so any
    // full-height rule inside makes each report grow the frame, which grows the
    // next report. The panel ratchets open with a band of empty space under the
    // content. Measuring the root element's own extent breaks that loop: it is
    // a function of the content and nothing else.
    function reportHeight() {
      if (heightPending) return;
      heightPending = true;
      requestAnimationFrame(() => {
        heightPending = false;
        if (!root) return;
        const rect = root.getBoundingClientRect();
        // Add the root's offset from the document top so any margin above it
        // is counted, and round up so a fractional layout never clips.
        const h = Math.ceil(rect.height + rect.top + window.scrollY);
        if (h === lastHeight) return;
        lastHeight = h;
        post({ type: 'height', height: h });
      });
    }

    // ---- token handshake --------------------------------------------------
    window.addEventListener('message', (e) => {
      if (e.source !== window.parent) return;
      const d = e.data;
      if (!d || d.proto !== PROTO || d.type !== 'token') return;
      // First token wins. A second one — from a parent that changed its mind,
      // or from a script that got hold of the frame handle — must not swap the
      // frame's identity out from under content already rendered for the first.
      if (started) return;
      started = true;
      parentOrigin = e.origin;
      exchange(String(d.token || ''));
    });

    // ---- exchange + refresh ----------------------------------------------
    async function exchange(token) {
      if (!token) { fail('no-token'); return; }
      try {
        let r;
        let body;
        await withDeadline(async (signal) => {
          r = await fetch(cfg.exchange, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ token: token, origin: parentOrigin }),
            // No cookie may ride along. Embed routes reject cookies outright,
            // but omitting them here means a misconfigured route never even
            // gets the chance to honour one.
            credentials: 'omit',
            signal: signal,
          });
          if (r.ok) body = await r.json();
        });
        if (!r.ok) { fail('exchange-failed', r.status); return; }
        applyGrant(body);
        await loadContent();
      } catch (err) {
        fail('exchange-error', err);
      }
    }

    function applyGrant(body) {
      grant = body && body.grant ? String(body.grant) : null;
      refreshFailures = 0;
      if (refreshTimer) clearTimeout(refreshTimer);
      refreshTimer = null;
      if (!grant || !cfg.refresh) return;
      const ms = Number(body.expires_in_ms);
      if (!Number.isFinite(ms) || ms <= 0) return;
      // Refresh at 80% of the remaining life, with a floor so a very short
      // grant cannot schedule a busy loop. setTimeout truncates delays to a
      // 32-bit int, so clamp before scheduling rather than after.
      const delay = Math.min(Math.max(Math.floor(ms * 0.8), 5000), 0x7fffffff);
      refreshTimer = setTimeout(refresh, delay);
    }

    async function refresh() {
      if (!grant) return;
      try {
        let r;
        let body;
        await withDeadline(async (signal) => {
          r = await fetch(cfg.refresh, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Gofastr-Embed': grant },
            credentials: 'omit',
            signal: signal,
          });
          if (r.ok) body = await r.json();
        });
        if (r.status === 401 || r.status === 403) {
          // The credential is genuinely finished: the absolute lifetime is over,
          // or the surface was reconfigured.
          expire();
          return;
        }
        if (!r.ok) {
          // Anything else is the SERVER having a bad moment, not the grant
          // ending. /__gofastr/embed-refresh answers 503 while a replica is
          // starting, and any proxy in front of it can answer 502. Treating
          // those as fatal expired every open frame on every customer page
          // during a rolling deploy, hours before their deadlines, recoverable
          // only by the visitor reloading the host page. Fall through to the
          // same backoff the network path uses.
          throw new Error('refresh failed: ' + r.status);
        }
        applyGrant(body);
      } catch (_) {
        // Transient network failure: retry, but not forever and not in
        // lockstep. An app that stays down would otherwise have every embed on
        // every customer page retrying together, every 15s, for as long as the
        // tabs stay open. Back off, jitter, and give up after a few attempts —
        // the parent is told, and a reload gets a fresh nonce.
        refreshFailures++;
        if (refreshFailures > 4) {
          expire();
          return;
        }
        const backoff = 15000 * refreshFailures;
        refreshTimer = setTimeout(refresh, backoff + Math.floor(Math.random() * 5000));
      }
    }

    // ---- content ----------------------------------------------------------
    async function loadContent() {
      if (!root) { fail('no-root'); return; }
      try {
        let r;
        let html;
        await withDeadline(async (signal) => {
          r = await fetch(cfg.content, {
            headers: { 'X-Gofastr-Embed': grant || '' },
            credentials: 'omit',
            signal: signal,
          });
          if (r.ok) html = await r.text();
        });
        if (!r.ok) { fail('content-failed', r.status); return; }
        root.innerHTML = html;
        root.setAttribute('data-fui-embed-state', 'ready');
        // The MutationObserver installed by boot handles hydration and
        // demand-loading modules for the injected subtree; component CSS is
        // not part of that pass, so scan for it explicitly.
        const G = window.__gofastr;
        if (G && G.scanAndLoadCSS) {
          G.scanAndLoadCSS(root);
          if (G.scheduleIdleLoads) G.scheduleIdleLoads();
        }
        // Two hydration passes run only at initial load or on SPA nav, and
        // neither is on the namespace: _runMountActions (which fires
        // data-action-mount, how an island populates itself) and
        // _injectSignalAria (which gives signal-bound text its aria-live).
        // Boot listens for gofastr:navigate to re-run both, so announcing the
        // swap through the same event is what makes injected content behave
        // like any other server-rendered screen. Without it an entity list
        // renders permanently empty inside the frame and live updates are
        // silent to screen readers.
        // The path is the SURFACE's app route, not the shell's URL. Boot's
        // gofastr:navigate listener re-fetches the widget catalog with
        // ?page=<path>, and a widget scoped with .Pages("/reports") is not
        // scoped to /__gofastr/embed/reports. This also runs AFTER the grant
        // is installed, so that catalog fetch is authenticated — the boot-time
        // one fires before the handshake and is expected to come back empty.
        const surfacePath = cfg.path || location.pathname;
        window.dispatchEvent(new CustomEvent('gofastr:navigate', {
          detail: { path: surfacePath, prevPath: surfacePath, cached: false },
        }));
        reportHeight();
      } catch (err) {
        fail('content-error', err);
      }
    }

    // ---- wiring -----------------------------------------------------------
    // Attach the grant to every same-origin fetch this document makes.
    //
    // The grant is the ONLY identity an embedded surface has — no cookie is
    // ever sent from inside a frame — so every request that would normally ride
    // on the session has to carry it instead. That is not just island RPC: the
    // demand-loaded modules (poll, toggle-action, optimistic-action, infinite
    // scroll, sortable list, intercept, widget state) each build their own
    // headers, and an earlier version of this hooked only rpc.js's builder, so
    // all of them fetched anonymously and silently swapped authenticated
    // content for a logged-out render.
    //
    // Wrapping fetch is what makes that class of miss impossible: a module
    // added next year is covered without knowing this exists. Scoped to
    // same-origin so the grant can never be attached to a third-party URL, and
    // installed only in the embed composition, so no other page pays for it.
    const _fetch = window.fetch.bind(window);
    window.fetch = (input, init) => {
      if (!grant) return _fetch(input, init);
      let url;
      try {
        url = new URL((input && input.url) || String(input), location.href);
      } catch (_) {
        return _fetch(input, init);
      }
      if (url.origin !== location.origin) return _fetch(input, init);
      const opts = Object.assign({}, init);
      const headers = new Headers((init && init.headers) || (input && input.headers) || undefined);
      headers.set('X-Gofastr-Embed', grant);
      opts.headers = headers;
      // Make "no cookie reaches an embed request" true at the source rather
      // than assumed. Callers pass credentials:'same-origin' (rpc, poll,
      // intercept all do), and in a SAME-SITE framing the browser really does
      // send the cookie — the one case the server goes to lengths to defend
      // against. Omitting here means the request never carries one to begin
      // with, so that defence is not the only thing standing between an embed
      // and the viewer's unrelated session.
      opts.credentials = 'omit';
      // Refuse to follow redirects on a credentialed request.
      //
      // Same-origin is decided from the URL we are GIVEN. A redirect changes
      // the origin after that check, and the browser strips only Authorization
      // across an origin change — a custom header rides along. So one open
      // redirect on the app (an OAuth start, a /out?url= tracker, a CDN
      // handoff) would hand the grant to whoever the redirect names, and the
      // grant is a bearer credential for its subject until its deadline.
      //
      // Nothing here needs to follow one: the framework answers a redirect on
      // an interactive request with the X-Gofastr-Location header, not a 3xx.
      opts.redirect = 'error';
      return _fetch(input, opts);
    };

    // Absence of the nav fragment is not enough on its own.
    //
    // Modules guard on `__gofastr.navigate` being present and fall back to a
    // hard `location.href` when it is not — which inside a frame is not a rare
    // race but the ONLY path. src/combobox.js does exactly that, so choosing an
    // option navigated the frame to an ordinary app route, whose CSP refuses to
    // be framed: blank panel, runtime gone, grant gone, and no way left to
    // report any of it.
    //
    // Defining a no-op navigator flips every one of those guards to the branch
    // that does nothing instead of the branch that destroys the embed — and
    // covers modules added later, for the same reason fetch is wrapped and
    // history is neutered here rather than at each call site.
    try {
      const NS = window.__gofastr || (window.__gofastr = {});
      NS.navigate = function (path) {
        console.warn('[gofastr/embed] ignored navigation to ' + path +
          ' — an embedded surface cannot leave its own screen');
      };
    } catch (_) {}

    // Never write to the host page's history.
    //
    // The embed composition omits the nav fragment, so there is no SPA
    // navigator in here — but pushState is not navigation and was not removed
    // with it. Widget and pane deep links call it, and so does any island whose
    // response carries X-Gofastr-Push-State. Inside a frame it appends to the
    // TOP-LEVEL browsing context's joint session history, which is the
    // customer's back button: open and close one deep-linked modal and the
    // visitor needs three Back presses to leave a page they never navigated
    // away from, the first two appearing to do nothing.
    //
    // Neutered here rather than at each call site for the same reason fetch is
    // wrapped here: a module added next year is covered without knowing this
    // exists.
    try {
      const noHistory = (name) => {
        const original = history[name];
        history[name] = function () {
          console.warn('[gofastr/embed] ignored history.' + name +
            ' — an embedded frame shares the host page\'s history');
          return undefined;
        };
        history[name].__gofastrOriginal = original;
      };
      noHistory('pushState');
      noHistory('replaceState');
    } catch (_) {}

    // An embed is one screen. Browser navigation would replace the shell and
    // discard its grant/runtime, so cancel ordinary links and native forms.
    // Capturing makes this reliable even when content stops propagation; the
    // event still reaches its own handlers, so RPC-backed controls keep working.
    function showBlockedNavigation(kind, destination) {
      const code = kind + '-navigation-blocked';
      if (root) {
        let notice = document.getElementById('gofastr-embed-navigation-error');
        if (!notice) {
          notice = document.createElement('p');
          notice.id = 'gofastr-embed-navigation-error';
          notice.setAttribute('role', 'alert');
          root.prepend(notice);
        }
        notice.textContent = kind === 'link'
          ? 'This panel cannot open links.'
          : 'This panel cannot submit this form.';
      }
      post({ type: 'error', code: code });
      console.warn('[gofastr/embed] ' + code + ': ' + destination);
    }

    function blockLinkNavigation(e) {
      const link = e.target && e.target.closest ? e.target.closest('a[href]') : null;
      if (!link) return;
      const href = link.getAttribute('href');
      if (!href || href.charAt(0) === '#') return;
      // A link that asks for a new tab gets one.
      //
      // Cancelling every link would be safe and unusable: an embedded
      // dashboard with "View the full report" is the ordinary case, and a new
      // tab destroys neither the frame nor the customer's page. Only
      // navigation that would replace one of them is refused.
      //
      // Opened through window.open with noopener rather than by letting the
      // browser handle it, so the new window cannot reach back through
      // window.opener to navigate the frame.
      if (link.target === '_blank') {
        e.preventDefault();
        const opened = window.open(href, '_blank', 'noopener');
        if (opened) { opened.opener = null; return; }
        // The browser refused the popup. Say so rather than appear inert.
        showBlockedNavigation('link', href);
        return;
      }
      e.preventDefault();
      showBlockedNavigation('link', href);
    }

    function blockFormNavigation(e) {
      const form = e.target;
      if (!form || form.tagName !== 'FORM') return;
      const enctype = (form.getAttribute('enctype') || '').toLowerCase();
      if (form.hasAttribute('data-fui-rpc') ||
          form.hasAttribute('data-fui-spa') ||
          enctype === 'application/json') return;
      e.preventDefault();
      showBlockedNavigation('form', form.action || '');
    }

    window.addEventListener('click', blockLinkNavigation, true);
    window.addEventListener('submit', blockFormNavigation, true);

    if (window.parent === window) {
      // Someone opened the embed URL directly. There is no parent to hand us a
      // nonce, so there is nothing to render — say so instead of spinning.
      fail('not-framed');
      return;
    }

    if (typeof ResizeObserver === 'function' && root) {
      // Observe the ROOT, not documentElement: observing the document means
      // observing the frame we are resizing, which is a feedback loop.
      new ResizeObserver(reportHeight).observe(root);
    }
    window.addEventListener('load', reportHeight);

    // Say something if the token never arrives.
    //
    // Every other failure here is reported; waiting was the one that was not,
    // so it presented as a panel stuck on "loading" with an empty root and
    // nothing in either console. The parent can drop the ping (a loader that
    // already handed a token to an earlier life of this document and refuses to
    // repeat itself), the host page can be slow, or the customer can have
    // removed the iframe's handler. All of them look identical from in here,
    // and all of them are better as an error than as a spinner.
    setTimeout(() => {
      if (!started) fail('no-token-timeout');
    }, 15000);

    post({ type: 'ready', surface: cfg.surface });
  })();
