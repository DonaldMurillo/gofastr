// GoFastr embed loader — the only GoFastr code that runs on a customer's page.
//
// It creates the iframe, hands the single-use nonce to it by postMessage, and
// keeps the frame's height in sync. Everything else happens inside the frame,
// where GoFastr is same-origin with itself. This file lands on a stranger's
// critical path, so its budget is the tightest in the repo — new behaviour
// belongs in boot-embed, not here.
//
// Usage:
//   <div id="reports"></div>
//   <script src="https://app.example.com/__gofastr/embed.js"
//           data-surface="reports" data-token="emb_…" data-target="#reports"></script>
//
// Omit data-target to place the frame where the script tag sits.
(() => {
  'use strict';
  const PROTO = 'gofastr-embed/1';

  // The app origin comes from THIS script's own src, which is the one thing on
  // the page we can trust to point at the app: the customer copied it from us.
  // Every postMessage that carries the nonce is addressed to it exactly, so a
  // token can never be delivered to another origin.
  function originOf(src) {
    try { return new URL(src, location.href).origin; } catch (_) { return null; }
  }

  function mount(opts) {
    const origin = opts.origin;
    const surface = opts.surface;
    const token = opts.token;
    const target = opts.target;
    if (!origin || !surface || !token || !target) return null;

    // Mount once per target. A double-mounted loader would create a second
    // frame and exchange the SAME single-use nonce twice; the server makes
    // that idempotent, but two frames rendering the same surface is still a
    // bug the customer would report as flicker.
    if (target.__gofastrEmbed) return target.__gofastrEmbed;

    const frame = document.createElement('iframe');
    // The customer's brand tokens ride in the URL, not the handshake, so the
    // frame links the right stylesheet in its FIRST response instead of
    // rendering in the app palette and swapping. They are colours, not secrets.
    let src = origin + '/__gofastr/embed/' + encodeURIComponent(surface);
    if (opts.theme) src += '?theme=' + encodeURIComponent(opts.theme);
    frame.src = src;
    frame.title = opts.title || ('Embedded ' + surface);
    // Scripts run the handshake/runtime; same-origin keeps app fetches and
    // storage on the app origin. No other capability is needed: in particular,
    // omitting top-navigation and forms keeps the frame inside this one screen.
    // The browser-enforced containment. Absent this attribute an embeddable
    // screen containing <a target="_top"> replaces the CUSTOMER's page when
    // clicked — proven in Chromium, and the failure that ends a customer
    // relationship.
    //
    //   allow-scripts      the runtime, handshake, hydration and RPC need it
    //   allow-same-origin  the frame IS the app's origin; without this it gets
    //                      an opaque one, app fetches become cross-origin and
    //                      storage throws
    //   allow-popups(+escape)  so target="_blank" links open a normal new tab
    //                      instead of being refused; a new tab replaces
    //                      nothing
    //
    // Deliberately ABSENT: allow-top-navigation and
    // allow-top-navigation-by-user-activation, which are what stop the frame
    // taking the customer's page; allow-forms, because native form submission
    // navigates the frame and every GoFastr form posts through fetch;
    // allow-modals and allow-downloads, which nothing here needs.
    //
    // allow-scripts + allow-same-origin together would let a frame remove its
    // own sandbox IF it were same-origin with this page. It is not: this
    // script runs on the customer's origin and the frame is the app's.
    frame.setAttribute('sandbox', 'allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox');
    frame.style.cssText = 'display:block;width:100%;border:0;';
    frame.style.height = (opts.height || 150) + 'px';
    // The nonce travels by postMessage, never in the URL, but the frame's URL
    // still names the surface — keep it out of the app's own logs' Referer.
    frame.setAttribute('referrerpolicy', 'no-referrer');
    frame.setAttribute('scrolling', 'no');
    if (opts.className) frame.className = opts.className;

    // Watchdog for the failures the frame cannot report, because in these the
    // frame document never runs script at all:
    //   - the customer's origin is not in the surface's allowlist, so
    //     frame-ancestors blocks the document (the single most common
    //     integration mistake)
    //   - the surface was renamed or the host has no signing key, so the shell
    //     answers 404/503
    //   - the customer's own CSP blocks frame-src
    // boot-embed's own timeout only covers the case where it loaded and ran.
    // Without this the customer sees an empty box and their error reporting
    // sees nothing.
    let ready = false;
    const watchdog = setTimeout(() => {
      if (ready) return;
      const detail = { type: 'error', code: 'no-ready' };
      if (typeof opts.onError === 'function') opts.onError(detail);
      console.warn('[gofastr/embed] surface "' + surface + '" never loaded. ' +
        'Most often the page origin (' + location.origin + ') is not in the ' +
        "surface's allowed origins.");
    }, 20000);

    const onMessage = (e) => {
      if (e.source !== frame.contentWindow) return;
      if (e.origin !== origin) return;
      const d = e.data;
      if (!d || d.proto !== PROTO) return;
      if (d.type === 'ready') {
        ready = true;
        clearTimeout(watchdog);
        // Answer EVERY ready, not just the first.
        //
        // A frame can re-run its document without the loader knowing: the
        // context-menu "Reload frame", a bfcache restore that re-navigates it,
        // a browser retry of a transient error. Its WindowProxy identity
        // survives all of those, so the source and origin checks above still
        // pass and the message is genuinely ours. Refusing to repeat the token
        // left that frame waiting for something that would never arrive —
        // empty root, state stuck at "loading", nothing in either console.
        //
        // Repeating costs nothing. The nonce is single-use at the server and
        // the exchange is idempotent inside the grant's life, so a second hand
        // over returns the same grant rather than a second identity.
        frame.contentWindow.postMessage({ proto: PROTO, type: 'token', token: token }, origin);
      } else if (d.type === 'height') {
        // Clamp. The frame is our own origin, but the host page is not ours to
        // break — a bad measurement should shrink an embed, never blow out the
        // customer's layout.
        const h = Number(d.height);
        if (Number.isFinite(h) && h > 0) frame.style.height = Math.min(Math.ceil(h), 20000) + 'px';
      } else if (d.type === 'error' || d.type === 'expired') {
        if (typeof opts.onError === 'function') opts.onError(d);
      }
    };
    window.addEventListener('message', onMessage);


    target.appendChild(frame);
    const handle = {
      frame: frame,
      destroy() {
        window.removeEventListener('message', onMessage);
        frame.remove();
        delete target.__gofastrEmbed;
      },
    };
    target.__gofastrEmbed = handle;
    return handle;
  }

  // Auto-mount from the script tag's data-* attributes.
  const self = document.currentScript;
  if (self) try {
    const origin = originOf(self.src);
    const sel = self.getAttribute('data-target');
    let target = null;
    if (sel) {
      target = document.querySelector(sel);
    } else if (self.__gofastrMounted) {
      // Mount once per SCRIPT TAG too, not only per target element. Without a
      // data-target each execution creates a FRESH container, so the
      // per-target guard has nothing to recognise and a re-executed snippet
      // would open a second frame exchanging the same single-use nonce.
      target = null;
    } else {
      self.__gofastrMounted = true;
      // No target: insert a container where the tag sits. document.currentScript
      // is only meaningful during synchronous execution, so this runs now.
      target = document.createElement('div');
      self.parentNode.insertBefore(target, self);
    }
    if (!target) {
      // A selector that matches nothing is the most common integration
      // mistake, and it is silent otherwise: no frame, no error, no clue.
      console.warn('[gofastr/embed] data-target "' + sel + '" matched no element');
    } else {
      const surface = self.getAttribute('data-surface');
      const token = self.getAttribute('data-token');
      // Name the missing attribute. A snippet whose template rendered an empty
      // data-token — the variable was misspelled, or minting failed server-side
      // — otherwise produced an empty container and absolute silence, which is
      // indistinguishable from the script never having loaded.
      if (!surface || !token) {
        console.warn('[gofastr/embed] missing ' +
          (!surface ? 'data-surface' : 'data-token') +
          ' — the snippet needs both, and data-token must be a freshly minted nonce');
      }
      mount({
        origin: origin,
        surface: surface,
        token: token,
        target: target,
        height: Number(self.getAttribute('data-height')) || 0,
        className: self.getAttribute('data-class') || '',
        title: self.getAttribute('data-title') || '',
        theme: self.getAttribute('data-theme') || '',
        // The auto-mount path had no error surface at all: onError was reachable
        // only through the programmatic API, so every failure inside the frame
        // (a spent nonce from a cached page, a refused origin) reached the
        // loader and was dropped. The frame's own console is cross-origin and
        // invisible to the customer's error reporting, so this is the only
        // place the message can land on their page.
        onError: (d) => {
          console.warn('[gofastr/embed] surface "' + surface + '" ' +
            (d && d.type === 'expired' ? 'credential expired' : 'failed to load') +
            (d && d.code ? ': ' + d.code : ''));
        },
      });
    }
  } catch (e) {
    // This runs on someone else's page. A malformed data-target selector
    // throws from querySelector, and a tag with no parent throws from
    // insertBefore — either would abort before window.GoFastrEmbed is defined
    // and surface in the customer's error reporting as a GoFastr exception on
    // their site. Contain it and say what happened.
    console.warn('[gofastr/embed] loader failed to auto-mount', e);
  }

  // Programmatic mounting, for hosts that render their page after this script
  // loads. Same code path — there is no second implementation to drift.
  window.GoFastrEmbed = {
    mount(opts) {
      const target = typeof opts.target === 'string' ? document.querySelector(opts.target) : opts.target;
      return mount({
        origin: opts.origin || (self ? originOf(self.src) : null),
        surface: opts.surface,
        token: opts.token,
        target: target,
        height: opts.height || 0,
        className: opts.className || '',
        title: opts.title || '',
        theme: opts.theme || '',
        onError: opts.onError,
      });
    },
  };
})();
